// Package installer runs the release installer bundled into the binary. The
// same script is published as a release asset for first-time installs, so
// upgrades and fresh installs share one tested code path.
package installer

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed install.sh
var script string

// Run downloads and installs a release over the running binary. An empty
// version means the newest release.
//
// The script is fed on stdin rather than written to a temp file, so there is no
// intermediate artifact to clean up or to have its permissions tampered with.
func Run(ctx context.Context, version string, out io.Writer) error {
	dir, err := installDir()
	if err != nil {
		return err
	}
	// install.sh's own writability fallback is inert once INSTALL_DIR is set,
	// so the check has to happen here to produce an error worth reading.
	if err := checkWritable(dir); err != nil {
		return err
	}

	args := []string{"-s", "--"}
	if version != "" {
		args = append(args, version)
	}
	cmd := exec.CommandContext(ctx, "sh", args...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = append(os.Environ(), "INSTALL_DIR="+dir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}
	return nil
}

// installDir is the directory holding the running binary, so an upgrade
// replaces it in place instead of leaving a second copy elsewhere on PATH.
func installDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Resolve symlinks so a linked binary upgrades its target, not the link.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe), nil
}

func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".infr-upgrade-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: re-run with sudo, or set INSTALL_DIR to a writable directory: %w", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}
