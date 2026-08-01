package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	adaptercli "inferencerig/adapters/cli"
	"inferencerig/bootstrap"
	"inferencerig/config"
	"inferencerig/core/rpc"
	"inferencerig/internal/buildinfo"
	"inferencerig/internal/logs"

	"github.com/spf13/cobra"
)

// Execute configures the default logger and runs the root command.
func Execute() error {
	slog.SetDefault(logs.New(slog.LevelInfo, os.Stderr))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return NewRootCommand().ExecuteContext(ctx)
}

// NewRootCommand builds the CLI.
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:               config.ProjectName,
		Short:             "Neutral control plane for local inference backends",
		Args:              cobra.NoArgs,
		Version:           buildinfo.Version,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
		RunE: func(command *cobra.Command, _ []string) error {
			return reportStartupFailure(command, runTUI(command, ""))
		},
	}
	rootCmd.AddCommand(versionCommand())
	rootCmd.AddCommand(serveCommand(), daemonCommand(), setupCommand(), tuiCommand(), webCommand(), serviceCommand())
	rootCmd.AddCommand(adaptercli.Commands(rpc.DialControl, bootstrap.ValidateConfig)...)
	return rootCmd
}
