package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	adaptercli "inferencerig/adapters/cli"
	"inferencerig/core/rpc"
	"inferencerig/core/setup"
)

// setupDialTimeout bounds the control-socket dial for the setup wizard.
const setupDialTimeout = 30 * time.Second

func setupCommand() *cobra.Command {
	var socket string
	command := &cobra.Command{
		Use: "setup", Short: "Create a canonical profile interactively", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			client, err := rpc.DialControl(adaptercli.ResolveSocket(socket), setupDialTimeout)
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
