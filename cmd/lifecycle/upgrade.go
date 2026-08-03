package lifecycle

import (
	"fmt"
	"io"

	"inferencerig/config"
	"inferencerig/internal/buildinfo"
	"inferencerig/internal/installer"
	"inferencerig/platform/process"

	"github.com/spf13/cobra"
)

// Package vars for the same reason as startDetached: these touch real
// processes and a real network, so tests drive the ordering through them.
var (
	stopDetached   = process.StopDetached
	statusDetached = process.StatusDetached
	runInstaller   = installer.Run
)

// UpgradeCommand replaces the running binary with a release build and restarts
// the daemons, which otherwise keep executing the old one.
//
// The order — stop, install, start — is forced by StopDetached's PID-reuse
// guard: it only signals a process whose executable is the same file (device +
// inode) as the caller's. `install` replaces the binary by creating a NEW
// inode, so a daemon stopped after the install no longer matches, and
// StopDetached would silently decline to stop it, leaving a stale process
// holding the port while a second one fails to bind.
func UpgradeCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "upgrade [version]",
		Aliases: []string{"update"},
		Short:   "Install the latest release and restart running services",
		Long: "Downloads a release, verifies its checksum, replaces this binary, and\n" +
			"restarts whichever of the control daemon and web gateway were running.\n" +
			"Pass a version (e.g. v0.1.0) to install that release instead of the newest.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			// A dev build has no release it came from; silently replacing a
			// locally built binary with a release would be a nasty surprise.
			if buildinfo.Version == "dev" {
				return fmt.Errorf("this is a development build, not an installed release; build from source instead")
			}
			version := ""
			if len(args) == 1 {
				version = args[0]
			}
			return runUpgrade(command, version)
		},
	}
}

func runUpgrade(command *cobra.Command, version string) error {
	out := command.OutOrStdout()

	// Recorded before stopping, because stopping is what makes them look down.
	running := runningServices()

	stopped := stopServices(out, running)

	if err := runInstaller(command.Context(), version, out); err != nil {
		// Bring back whatever was taken down, so a failed download does not
		// also cost the user their running daemons. Any restart failure is
		// already printed; the install error is the one worth returning.
		_ = startServices(out, stopped)
		return err
	}
	return startServices(out, stopped)
}

func runningServices() []string {
	active := []string{}
	for _, name := range config.DefaultStartupServices() {
		if status, err := statusDetached(config.ServiceProcessName(name)); err == nil && status.Running {
			active = append(active, name)
		}
	}
	return active
}

// stopServices returns the services actually stopped, so a service that refused
// to stop is not later "restarted" into a port conflict with itself.
func stopServices(out io.Writer, services []string) []string {
	stopped := []string{}
	for _, name := range services {
		if err := stopDetached(config.ServiceProcessName(name)); err != nil {
			_, _ = fmt.Fprintf(out, "warning: could not stop %s: %v\n", name, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "stopped %s\n", name)
		stopped = append(stopped, name)
	}
	return stopped
}

// startServices reports the first failure but keeps going: leaving the other
// service down as well would compound one problem into two.
func startServices(out io.Writer, services []string) error {
	var failed error
	for _, name := range services {
		if err := startDetached(config.ServiceProcessName(name), config.ServiceArgs(name)...); err != nil {
			failed = err
			_, _ = fmt.Fprintf(out, "warning: could not start %s: %v\n", name, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "started %s\n", name)
	}
	return failed
}
