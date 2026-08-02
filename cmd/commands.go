package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

// daemonLifecycleCommands returns commands that start, manage, or embed the
// daemon process itself (serve/daemon/setup/tui/web/service).
func daemonLifecycleCommands() []*cobra.Command {
	return []*cobra.Command{
		serveCommand(),
		daemonCommand(),
		setupCommand(),
		tuiCommand(),
		webCommand(),
		serviceCommand(),
	}
}

// standaloneCommands returns commands that don't require a running daemon.
func standaloneCommands(validate func(context.Context) error) []*cobra.Command {
	return []*cobra.Command{
		versionCommand(),
		doctorCommand(validate),
	}
}
