package llamacpp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"inferencerig/backends"
)

func TestInstallIdempotentAndActiveExecutable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "engine")
	b := New(Options{EngineRoot: root, Fetcher: stubFetcher{version: "b1"}})

	first, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != "b1" || !first.Changed {
		t.Fatalf("first install = %#v", first)
	}
	if info, err := os.Stat(first.Path); err != nil || info.IsDir() {
		t.Fatalf("installed executable missing: %v", err)
	}
	if exe, ok := b.installer.activeExecutable(); !ok || exe != first.Path {
		t.Fatalf("activeExecutable = %q, %v", exe, ok)
	}
	second, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatalf("second install reported Changed: %#v", second)
	}
}

func TestInstallStatusFindsHostExecutable(t *testing.T) {
	executable := filepath.Join(t.TempDir(), defaultExecutable)
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	status, err := New(Options{Executable: executable, EngineRoot: t.TempDir()}).InstallStatus(context.Background())
	if err != nil || !status.Installed || status.Managed || status.Path != executable {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
}

func TestInstallUpgradeAndRetention(t *testing.T) {
	root := filepath.Join(t.TempDir(), "engine")
	b := New(Options{EngineRoot: root, Fetcher: stubFetcher{version: "b1"}})
	if _, err := b.Install(context.Background(), backends.InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	firstDir := onlyVersionDir(t, root, "b1")
	if _, err := b.Install(context.Background(), backends.InstallOptions{Version: "b2", Upgrade: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstDir); err != nil {
		t.Fatalf("b1 removed too early (should be previous): %v", err)
	}
	third, err := b.Install(context.Background(), backends.InstallOptions{Version: "b3", Upgrade: true})
	if err != nil {
		t.Fatal(err)
	}
	if third.Version != "b3" || !third.Changed {
		t.Fatalf("third install = %#v", third)
	}
	if _, err := os.Stat(firstDir); !os.IsNotExist(err) {
		t.Fatalf("oldest install retained after retention: %v", err)
	}
	st, err := readInstallState(root)
	if err != nil || st.Previous == nil || st.Previous.Version != "b2" {
		t.Fatalf("state previous = %#v, err = %v", st.Previous, err)
	}
}

func TestInstallUpgradeWithoutInstallFails(t *testing.T) {
	b := New(Options{EngineRoot: filepath.Join(t.TempDir(), "engine"), Fetcher: stubFetcher{version: "b1"}})
	if _, err := b.Install(context.Background(), backends.InstallOptions{Upgrade: true}); err == nil {
		t.Fatal("Upgrade with nothing installed should fail")
	}
}

// Ported from llamarig core/llamainstall/llamainstall_test.go TestReleaseAsset.
func TestSelectAsset(t *testing.T) {
	rel := Release{Version: "b123", Assets: []ReleaseAsset{
		{Name: "llama-b123-bin-ubuntu-x64.tar.gz"},
		{Name: "llama-b123-bin-ubuntu-vulkan-arm64.tar.gz"},
		{Name: "llama-b123-bin-ubuntu-rocm-7.0-x64.tar.gz"},
		{Name: "llama-b123-bin-macos-arm64.tar.gz"},
	}}
	cases := []struct {
		goos, goarch string
		accel        Accel
		want         string
	}{
		{"linux", "amd64", AccelCPU, rel.Assets[0].Name},
		{"linux", "arm64", AccelVulkan, rel.Assets[1].Name},
		{"linux", "amd64", AccelROCm, rel.Assets[2].Name},
		{"darwin", "arm64", AccelMetal, rel.Assets[3].Name},
	}
	for _, c := range cases {
		got, err := (&githubFetcher{goos: c.goos, goarch: c.goarch}).selectAsset(rel, c.accel)
		if err != nil || got.Name != c.want {
			t.Errorf("%s/%s/%s: got %q, %v; want %q", c.goos, c.goarch, c.accel, got.Name, err, c.want)
		}
	}
}

// Ported from llamarig core/llamainstall/llamainstall_test.go TestDetectPriority.
func TestDetectPriority(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	writeCommand(t, dir, "nvidia-smi", 0)
	writeCommand(t, dir, "rocminfo", 0)
	writeCommand(t, dir, "vulkaninfo", 0)
	i := &installer{goos: "linux"}
	if got := i.detect(context.Background()); got != AccelCUDA {
		t.Fatalf("detect = %s, want cuda", got)
	}
	writeCommand(t, dir, "nvidia-smi", 1)
	if got := i.detect(context.Background()); got != AccelROCm {
		t.Fatalf("detect = %s, want rocm", got)
	}
	writeCommand(t, dir, "rocminfo", 1)
	if got := i.detect(context.Background()); got != AccelVulkan {
		t.Fatalf("detect = %s, want vulkan", got)
	}
}

func writeCommand(t *testing.T, dir, name string, exit int) {
	t.Helper()
	content := fmt.Sprintf("#!/bin/sh\nexit %d\n", exit)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func onlyVersionDir(t *testing.T, root, version string) string {
	t.Helper()
	dir := filepath.Join(root, version)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("version dir %q missing: %v", dir, err)
	}
	return dir
}
