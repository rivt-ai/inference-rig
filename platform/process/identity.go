package process

import (
	"context"
	"fmt"
	"os"

	gopsprocess "github.com/shirou/gopsutil/v4/process"
)

// SameExecutable reports whether pid is running the binary at path.
//
// PIDs are recycled, so "a process with this PID exists" is not evidence that
// it is the process the PID file recorded. Comparing by inode rather than by
// path name also survives a binary that has been replaced underneath a running
// process, which a string compare would miss.
func SameExecutable(ctx context.Context, pid int, path string) (bool, error) {
	proc, err := gopsprocess.NewProcess(int32(pid))
	if err != nil {
		return false, err
	}
	actual, err := proc.ExeWithContext(ctx)
	if err != nil {
		return false, err
	}
	wantInfo, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	gotInfo, err := os.Stat(actual)
	if err != nil {
		return false, err
	}
	if !os.SameFile(wantInfo, gotInfo) {
		return false, fmt.Errorf("pid %d is running %s, not %s", pid, actual, path)
	}
	return true, nil
}
