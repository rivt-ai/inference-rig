package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"inferencerig/bootstrap"
	"inferencerig/config"
	"inferencerig/platform/process"
)

func serveCommand() *cobra.Command {
	var detach bool
	command := &cobra.Command{
		Use: "serve", Short: "Run the canonical control daemon", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if detach {
				return process.StartDetached(config.ProjectName, "serve")
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

func daemonCommand() *cobra.Command {
	command := &cobra.Command{Use: "daemon", Short: "Manage a detached control daemon"}
	command.AddCommand(
		&cobra.Command{
			Use: "status", Args: cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				status, err := process.StatusDetached(config.ProjectName)
				if err != nil {
					return err
				}
				if !status.Running {
					_, err = fmt.Fprintln(command.OutOrStdout(), "stopped")
					return err
				}
				_, err = fmt.Fprintf(command.OutOrStdout(), "running pid=%d uptime=%s\n", status.PID, status.Uptime)
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
