package process

import "golang.org/x/sys/unix"

// peerPID reads SO_PEERCRED, which Linux fills in with the credentials the
// peer had when the connection was made.
func peerPID(fd uintptr) (int, error) {
	cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return int(cred.Pid), nil
}
