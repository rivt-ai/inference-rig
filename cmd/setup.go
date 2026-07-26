package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"inferencerig/config"
	"inferencerig/core/rpc"
	"inferencerig/core/setup"
)

func setupCommand() *cobra.Command {
	var socket string
	command := &cobra.Command{
		Use: "setup", Short: "Create a canonical profile interactively", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			path := socket
			if path == "" {
				path = os.Getenv(config.ProjectSocketEnv)
			}
			client, err := rpc.DialControl(path, 30*time.Second)
			if err != nil {
				return err
			}
			profile, err := setup.NewWizard(client).RunInteractive(
				command.Context(), command.InOrStdin(), command.OutOrStdout(),
			)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "created profile %s\n", profile.GetName())
			return err
		},
	}
	command.Flags().StringVar(&socket, "socket", "", "control Unix socket")
	return command
}
