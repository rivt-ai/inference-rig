package llamacpp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNoPrebuilt marks a host/accelerator with no matching prebuilt asset.
var ErrNoPrebuilt = errors.New("no matching llama.cpp prebuilt")

const defaultReleaseAPI = "https://api.github.com/repos/ggml-org/llama.cpp/releases"

// githubFetcher provisions llama.cpp from official GitHub prebuilt releases.
// Ported from llamarig core/llamainstall (backend.go, archive.go). Source
// builds (cmake) are intentionally not ported in Phase 6 (see Phase-6 notes).
type githubFetcher struct {
	goos, goarch string
	apiBase      string
	client       *http.Client
}

func (f *githubFetcher) http() *http.Client {
	if f.client != nil {
		return f.client
	}
	return &http.Client{Timeout: 30 * time.Minute}
}

func (f *githubFetcher) base() string {
	if f.apiBase != "" {
		return f.apiBase
	}
	return defaultReleaseAPI
}

// Resolve fetches release metadata (latest when version is empty).
func (f *githubFetcher) Resolve(ctx context.Context, _ Accel, version string) (Release, error) {
	endpoint := f.base() + "/latest"
	if version != "" {
		endpoint = f.base() + "/tags/" + version
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := f.http().Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("resolve llama.cpp release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("resolve llama.cpp release: %s", resp.Status)
	}
	return decodeRelease(resp.Body)
}

func decodeRelease(r io.Reader) (Release, error) {
	var raw struct {
		TagName    string `json:"tag_name"`
		TarballURL string `json:"tarball_url"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return Release{}, err
	}
	if raw.TagName == "" {
		return Release{}, fmt.Errorf("llama.cpp release metadata is incomplete")
	}
	rel := Release{Version: raw.TagName, TarballURL: raw.TarballURL}
	for _, a := range raw.Assets {
		rel.Assets = append(rel.Assets, ReleaseAsset{Name: a.Name, URL: a.URL, Digest: a.Digest, Size: a.Size})
	}
	return rel, nil
}

// Fetch downloads and extracts the prebuilt asset for accel, returning the
// staged llama-server path.
func (f *githubFetcher) Fetch(ctx context.Context, rel Release, accel Accel, dir string, progress io.Writer) (string, error) {
	asset, err := f.selectAsset(rel, accel)
	if err != nil {
		return "", err
	}
	if asset.Size <= 0 || !strings.HasPrefix(asset.Digest, "sha256:") {
		return "", fmt.Errorf("release asset %s lacks a SHA256 digest or size", asset.Name)
	}
	if _, err := fmt.Fprintln(progress, "download "+asset.Name+"..."); err != nil {
		return "", err
	}
	archive := filepath.Join(dir, "release.tar.gz")
	hash, size, err := download(ctx, f.http(), asset.URL, archive)
	if err != nil {
		return "", err
	}
	if size != asset.Size || !strings.EqualFold(hash, strings.TrimPrefix(asset.Digest, "sha256:")) {
		return "", fmt.Errorf("release asset integrity check failed")
	}
	if _, err := fmt.Fprintln(progress, "extract prebuilt..."); err != nil {
		return "", err
	}
	if err := extractTarGz(ctx, archive, dir); err != nil {
		return "", err
	}
	return findExecutable(dir)
}

// selectAsset chooses the prebuilt tarball for the host/accelerator.
func (f *githubFetcher) selectAsset(rel Release, accel Accel) (ReleaseAsset, error) {
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[f.goarch]
	platform := "ubuntu"
	if f.goos == "darwin" {
		platform = "macos"
	}
	part := ""
	if accel != AccelCPU && accel != AccelMetal {
		part = "-" + string(accel)
	}
	name := fmt.Sprintf("llama-%s-bin-%s%s-%s.tar.gz", rel.Version, platform, part, arch)
	for _, candidate := range rel.Assets {
		matchesROCm := accel == AccelROCm &&
			strings.HasPrefix(candidate.Name, strings.TrimSuffix(name, arch+".tar.gz")) &&
			strings.HasSuffix(candidate.Name, "-"+arch+".tar.gz")
		if candidate.Name == name || matchesROCm {
			return candidate, nil
		}
	}
	return ReleaseAsset{}, fmt.Errorf("%w for %s/%s/%s", ErrNoPrebuilt, f.goos, f.goarch, accel)
}

func download(ctx context.Context, client *http.Client, url, destination string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, hash), resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return "", 0, copyErr
	}
	return hex.EncodeToString(hash.Sum(nil)), size, closeErr
}

func extractTarGz(ctx context.Context, archive, destination string) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "tar", "-xzf", archive, "-C", destination).CombinedOutput()
	if err != nil {
		return fmt.Errorf("extract archive: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func findExecutable(root string) (string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Name() == defaultExecutable {
			matches = append(matches, path)
		}
		return err
	})
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("expected one %s, found %d", defaultExecutable, len(matches))
	}
	return matches[0], nil
}
