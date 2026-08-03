package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"inferencerig/cmd/lifecycle"
	"inferencerig/cmd/standalone"
)

// daemonLifecycleCommands returns commands that start, manage, or embed the
// daemon process itself (serve/daemon/setup/tui/web/service).
func daemonLifecycleCommands() []*cobra.Command {
	return []*cobra.Command{
		lifecycle.ServeCommand(),
		lifecycle.DaemonCommand(),
		lifecycle.SetupCommand(),
		lifecycle.TuiCommand(),
		lifecycle.WebCommand(),
		lifecycle.ServiceCommand(),
		lifecycle.UpgradeCommand(),
	}
}

// standaloneCommands returns commands that don't require a running daemon.
func standaloneCommands(validate func(context.Context) error) []*cobra.Command {
	return []*cobra.Command{
		standalone.VersionCommand(),
		standalone.DoctorCommand(validate),
	}
}
