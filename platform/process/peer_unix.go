//go:build linux || darwin

package process

import (
	"errors"
	"net"
	"time"
)

// SocketPeerPID reports the PID of the process listening on a Unix socket, by
// asking the kernel for the peer credentials of a connection to it.
//
// This is the only way to identify a running daemon that has no PID file: the
// socket itself is the evidence, and the kernel records who is on the other
// end of it. Scraping `ss` or `lsof` would answer the same question by parsing
// another program's output, which is neither always installed nor stable.
func SocketPeerPID(path string, timeout time.Duration) (int, error) {
	conn, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()
	// net.DialTimeout on a unix address always yields a *net.UnixConn.
	raw, err := conn.(*net.UnixConn).SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var credErr error
	if err := raw.Control(func(fd uintptr) { pid, credErr = peerPID(fd) }); err != nil {
		return 0, err
	}
	if credErr != nil {
		return 0, credErr
	}
	if pid <= 0 {
		return 0, errors.New("the kernel reported no peer pid for " + path)
	}
	return pid, nil
}
