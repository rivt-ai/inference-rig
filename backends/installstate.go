package backends

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"inferencerig/config"
	"inferencerig/platform/filedoc"
)

// stateFileName is the managed-install state document every backend keeps at
// the root of its engine directory.
const stateFileName = "state.json"

// installStateMode keeps the state document owner-readable only; it records
// local filesystem paths for a managed install.
const installStateMode = 0o600

// ErrNoPreviousInstall marks a rollback with no recorded install to return to.
var ErrNoPreviousInstall = errors.New("no previous managed install recorded")

// InstallRecord is the machine-readable record of one managed engine
// installation. It is the single neutral shape every backend writes and the
// release receipt and diagnostics read; engine specifics survive only as opaque
// strings in Source, Digest and Accelerator.
type InstallRecord struct {
	Backend string `json:"backend"`
	Version string `json:"version"`
	// Source is where the payload came from — a release asset URL, a package
	// index, a lock file. Free-form but stable per backend.
	Source string `json:"source"`
	// Digest is the verified content digest of the payload, prefixed with its
	// algorithm ("sha256:..."), when the backend has one.
	Digest string `json:"digest,omitempty"`
	// Platform is the GOOS/GOARCH the payload was installed for.
	Platform string `json:"platform"`
	// Accelerator is the compute backend the payload was selected for, when the
	// engine has that axis.
	Accelerator string    `json:"accelerator,omitempty"`
	Directory   string    `json:"directory"`
	Executable  string    `json:"executable"`
	InstalledAt time.Time `json:"installed_at"`
}

// InstallState is the document at <root>/state.json: the active installation
// and the one a rollback returns to. Retention keeps exactly these two.
type InstallState struct {
	Active   *InstallRecord `json:"active,omitempty"`
	Previous *InstallRecord `json:"previous,omitempty"`
}

// EngineRoot is where backend name keeps its managed install and its state
// document. It is shared rather than per-backend so anything reading install
// records — a release receipt, diagnostics — can find them from the backend
// name alone.
func EngineRoot(name string) (string, error) {
	home, err := config.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "engine", name), nil
}

// ReadInstallState decodes root/state.json. A missing file yields the zero
// value and no error, so a first install needs no special case.
func ReadInstallState(root string) (InstallState, error) {
	var state InstallState
	data, err := os.ReadFile(filepath.Join(root, stateFileName))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("read install state: %w", err)
	}
	return state, nil
}

// WriteInstallState atomically replaces root/state.json with state.
func WriteInstallState(root string, state InstallState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	_, err = filedoc.WriteFile(
		filepath.Join(root, stateFileName),
		string(data)+"\n",
		filedoc.WriteOptions{Perm: installStateMode},
	)
	return err
}

// RollbackInstall returns root's managed install to its previous record: verify
// proves that install is still usable (a backend runs its own probe), then the
// two records swap, so a second rollback undoes the first. Nothing is written
// when verify fails, which keeps a broken previous install from becoming the
// active one.
func RollbackInstall(ctx context.Context, root string, verify func(context.Context, InstallRecord) error) (InstallResult, error) {
	state, err := ReadInstallState(root)
	if err != nil {
		return InstallResult{}, err
	}
	if state.Previous == nil {
		return InstallResult{}, ErrNoPreviousInstall
	}
	restored := *state.Previous
	if err := verify(ctx, restored); err != nil {
		return InstallResult{}, fmt.Errorf("previous install %s is not usable: %w", restored.Version, err)
	}
	if err := WriteInstallState(root, InstallState{Active: &restored, Previous: state.Active}); err != nil {
		return InstallResult{}, err
	}
	return InstallResult{
		Version: restored.Version, Path: restored.Executable, Changed: true,
		Message: "rolled back to " + restored.Backend + " " + restored.Version,
	}, nil
}

// DigestVerification reports whether an installed payload still matches the
// digest recorded when it was installed.
type DigestVerification struct {
	Path      string `json:"path"`
	Algorithm string `json:"algorithm"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
	Matched   bool   `json:"matched"`
	Skipped   bool   `json:"skipped"`
	Reason    string `json:"reason,omitempty"`
}

// InstallDigestVerifier is the optional backend facet that re-hashes what it
// installed and compares against the record.
//
// It is a facet rather than shared code because only the backend knows what its
// digest covers: llama.cpp records the hash of an executable, MLX the hash of a
// requirements lock. A neutral verifier would have to switch on backend name to
// know which, and nothing outside a backend is allowed that knowledge.
type InstallDigestVerifier interface {
	VerifyInstall(ctx context.Context, record InstallRecord) (DigestVerification, error)
}

// VerifyRecordedDigest compares one file against record.Digest.
//
// A record with no digest is skipped rather than failed: engines installed
// before digests were kept have nothing to compare against, and reporting that
// as corruption would be a lie. The caller names the file, because only the
// backend knows what its own digest covers.
func VerifyRecordedDigest(record InstallRecord, path string) (DigestVerification, error) {
	result := DigestVerification{Path: path, Algorithm: "sha256", Expected: record.Digest}
	algorithm, expected, found := strings.Cut(record.Digest, ":")
	if !found || algorithm != "sha256" || expected == "" {
		result.Skipped, result.Reason = true, "no sha256 digest was recorded for this install"
		return result, nil
	}
	if path == "" {
		result.Skipped, result.Reason = true, "the install record names no file to verify"
		return result, nil
	}
	actual, err := filedoc.SHA256File(path)
	if err != nil {
		return result, err
	}
	result.Expected, result.Actual = expected, actual
	result.Matched = actual == expected
	return result, nil
}
