package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"inferencerig/config"
	"inferencerig/core/rpc"
	"inferencerig/core/setup"
	"inferencerig/platform/process"
)

func setupCommand() *cobra.Command {
	return &cobra.Command{
		Use: "setup", Short: "Configure InferenceRig interactively", Args: cobra.NoArgs,
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(command *cobra.Command, args []string) error {
			return reportStartupFailure(command, runSetup(command, args))
		},
	}
}

func runSetup(command *cobra.Command, _ []string) error {
	if os.Getenv(config.ProjectConfigEnv) != "" {
		return nil
	}
	client, err := rpc.DialControl("", 30*time.Second)
	if err != nil {
		return err
	}
	started, err := ensureControl(command.Context(), client)
	if err != nil {
		return err
	}
	result, err := setup.NewWizard(client).Rerun(command.Context(), command.InOrStdin(), command.OutOrStdout())
	if err != nil {
		return handleSetupError(command, started, err)
	}
	if result.Skipped {
		return nil
	}
	return restartControl(command.Context(), client)
}

func handleSetupError(command *cobra.Command, started bool, err error) error {
	if started {
		_ = process.StopDetached(config.ProjectName)
	}
	if errors.Is(err, setup.ErrCancelled) {
		_, _ = fmt.Fprintln(command.OutOrStdout(), "setup cancelled")
		return nil
	}
	return err
}
