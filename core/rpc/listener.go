// Package rpc holds the transport-generic pieces of the local control-plane
// RPC: the Unix-socket listener, the client transport, an HTTP server wrapper,
// and a request-validation interceptor. The canonical proto service and its
// generated client/handler land in a later phase; nothing here depends on
// generated code.
package rpc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"inferencerig/config"
)

// NewControlListener creates the local control Unix-socket listener. It creates
// the socket directory with 0700 permissions, removes a stale socket left by a
// crashed process, listens, and tightens the socket to 0600. It returns the
// listener and the resolved socket path.
func NewControlListener() (net.Listener, string, error) {
	path, err := config.ControlSocketPath()
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create control rpc socket dir: %w", err)
	}
	// MkdirAll does not tighten permissions on an existing directory.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, "", fmt.Errorf("chmod control rpc socket dir: %w", err)
	}
	if err := removeStaleSocket(path); err != nil {
		return nil, "", err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, "", fmt.Errorf("listen control rpc socket %q: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, "", fmt.Errorf("chmod control rpc socket %q: %w", path, err)
	}
	return listener, path, nil
}

func removeStaleSocket(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat control rpc socket %q: %w", path, err)
	}
	conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("control rpc socket %q is already in use", path)
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return fmt.Errorf("control rpc socket %q dial timed out (likely in use): %w", path, err)
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return fmt.Errorf("control rpc socket %q is already in use (permission denied)", path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale control rpc socket %q: %w", path, err)
	}
	return nil
}
