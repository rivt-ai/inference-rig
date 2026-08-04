package lifecycle

import (
	"fmt"

	"github.com/spf13/cobra"

	"inferencerig/bootstrap"
	"inferencerig/config"
	"inferencerig/internal/style"
	"inferencerig/platform/process"
)

func ServeCommand() *cobra.Command {
	var detach bool
	command := &cobra.Command{
		Use: "serve", Short: "Run the canonical control daemon", Args: cobra.NoArgs,
		// This command's stderr is the service log a failed start is read back
		// from, so a usage dump and a doubled error would bury the one line
		// that matters. The error is the output here.
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if detach {
				return ReportStartupFailure(command, startDetached(config.ProjectName, "serve"))
			}
			service, err := bootstrap.NewService()
			if err != nil {
				return err
			}
			return service.Run(command.Context())
		},
	}
	command.Flags().BoolVarP(&detach, "detach", "d", false, "run in the background")
	return command
}

func DaemonCommand() *cobra.Command {
	command := &cobra.Command{Use: "daemon", Short: "Manage a detached control daemon"}
	command.AddCommand(
		&cobra.Command{
			Use: "status", Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				status, err := process.StatusDetached(config.ProjectName)
				if err != nil {
					return err
				}
				paint := style.PainterFor(command.OutOrStdout())
				if !status.Running {
					_, err = fmt.Fprintln(command.OutOrStdout(), paint(style.ErrorStyle, "stopped"))
					return err
				}
				_, err = fmt.Fprintf(command.OutOrStdout(), "%s pid=%d uptime=%s\n",
					paint(style.SuccessStyle, "running"), status.PID, status.Uptime)
				return err
			},
		},
		&cobra.Command{
			Use: "stop", Args: cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				return process.StopDetached(config.ProjectName)
			},
		},
	)
	return command
}
