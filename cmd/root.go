package cmd

import (
	"context"
	"log/slog"

	adaptercli "inferencerig/adapters/cli"
	"inferencerig/config"
	"inferencerig/core/rpc"
	"inferencerig/internal/buildinfo"
	"inferencerig/internal/logs"

	"github.com/spf13/cobra"
)

// Execute configures the default logger and runs the root command.
func Execute() error {
	slog.SetDefault(logs.New(slog.LevelInfo))
	return NewRootCommand().ExecuteContext(context.Background())
}

// NewRootCommand builds the CLI. Subcommands are added here as the control
// plane grows (serve, gateway, profiles, runtime, …); the bootstrap ships the
// version command only.
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:               config.ProjectName,
		Short:             "Neutral control plane for local inference backends",
		Args:              cobra.NoArgs,
		Version:           buildinfo.Version,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	}
	rootCmd.AddCommand(versionCommand())
	rootCmd.AddCommand(adaptercli.Commands(rpc.DialControl)...)
	return rootCmd
}
