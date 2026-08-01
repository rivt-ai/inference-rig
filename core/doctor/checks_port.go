package doctor

import (
	"context"
	"fmt"
	"net"
	"strconv"

	gopsnet "github.com/shirou/gopsutil/v4/net"
	gopsprocess "github.com/shirou/gopsutil/v4/process"

	"inferencerig/platform/pidfile"
)

// checkPort reports whether the configured listen address is available.
//
// The probe is a bind-and-close, which is instant. Naming the process holding
// an occupied port needs a full connection table, which parses /proc on Linux
// and shells out to lsof on macOS — so that only runs when there is actually a
// conflict to explain.
func checkPort(ctx context.Context, e *env) Check {
	const id, title = "port.listen", "listen address"
	if e.loadErr != nil {
		return skip(id, title, "configuration could not be loaded")
	}
	addr := e.cfg.ListenAddr
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		_ = listener.Close()
		return ok(id, title, addr+" is available")
	}
	// Our own daemon holding its port is the expected state, not a conflict.
	if pid, exists, readErr := pidfile.New(pidFilePath(e.paths)).Read(); readErr == nil && exists && pidfile.Alive(pid) {
		return ok(id, title, fmt.Sprintf("%s is held by this installation (pid %d)", addr, pid))
	}
	return fail(id, title, addr+" is already in use").
		withDetail(describePortOwner(ctx, addr, err))
}

// describePortOwner names the process on the port, falling back to the bind
// error when the connection table is unavailable — that lookup needs privileges
// on some systems, and a partial answer beats none.
func describePortOwner(ctx context.Context, addr string, bindErr error) string {
	_, portText, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return bindErr.Error()
	}
	port, convErr := strconv.Atoi(portText)
	if convErr != nil {
		return bindErr.Error()
	}
	pid, found := findPortOwner(ctx, port)
	if !found {
		return bindErr.Error() + "\nThe owning process could not be identified."
	}
	detail := fmt.Sprintf("Port %d is held by pid %d", port, pid)
	if proc, err := gopsprocess.NewProcess(int32(pid)); err == nil {
		if name, err := proc.NameWithContext(ctx); err == nil {
			detail += " (" + name + ")"
		}
	}
	return detail + "."
}

func findPortOwner(ctx context.Context, port int) (int, bool) {
	connections, err := gopsnet.ConnectionsWithContext(ctx, "inet")
	if err != nil {
		return 0, false
	}
	for _, connection := range connections {
		if int(connection.Laddr.Port) == port && connection.Status == "LISTEN" && connection.Pid > 0 {
			return int(connection.Pid), true
		}
	}
	return 0, false
}
