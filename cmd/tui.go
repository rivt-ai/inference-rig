package cmd

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	adaptertui "inferencerig/adapters/tui"
	"inferencerig/config"
	"inferencerig/core/rpc"
)

func tuiCommand() *cobra.Command {
	var socket string
	command := &cobra.Command{
		Use: "tui", Short: "Open the interactive control dashboard", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			path := socket
			if path == "" {
				path = os.Getenv(config.ProjectSocketEnv)
			}
			client, err := rpc.DialControl(path, 30*time.Second)
			if err != nil {
				return err
			}
			return adaptertui.RunInteractive(command.Context(), command.InOrStdin(), command.OutOrStdout(), client)
		},
	}
	command.Flags().StringVar(&socket, "socket", "", "control Unix socket")
	return command
}
