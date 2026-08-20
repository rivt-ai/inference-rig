package process

import "golang.org/x/sys/unix"

// peerPID reads LOCAL_PEERPID, the Darwin equivalent of Linux's SO_PEERCRED
// for the one field this package needs.
func peerPID(fd uintptr) (int, error) {
	return unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
}
