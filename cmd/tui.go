package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	adaptertui "inferencerig/adapters/tui"
	"inferencerig/config"
	"inferencerig/core/rpc"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	"inferencerig/core/setup"
	"inferencerig/internal/logs"
	"inferencerig/platform/audit"
	"inferencerig/platform/process"
)

func tuiCommand() *cobra.Command {
	var socket string
	command := &cobra.Command{
		Use: "tui", Short: "Open the interactive control dashboard", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runTUI(command, socket)
		},
	}
	command.Flags().StringVar(&socket, "socket", "", "control Unix socket")
	return command
}

func runTUI(command *cobra.Command, socket string) error {
	// The TUI owns the terminal for its full-screen render, so the shared
	// logger (default: stderr) must not write there — a stray log line
	// corrupts the frame. Redirect it to a file for the life of this process.
	if err := redirectLogsToFile("tui"); err != nil {
		return err
	}
	path := socket
	if path == "" {
		path = os.Getenv(config.ProjectSocketEnv)
	}
	local := path == ""
	client, err := rpc.DialControl(path, 30*time.Second)
	if err != nil {
		return err
	}
	if local {
		cancelled, err := runFirstSetup(command, client)
		if err != nil {
			return err
		}
		if cancelled {
			return nil
		}
	}
	return adaptertui.RunInteractive(command.Context(), command.InOrStdin(), command.OutOrStdout(), adaptertui.Options{
		Client: client, ManageLocalServices: local,
	})
}

func runFirstSetup(command *cobra.Command, client controlv1connect.ControlServiceClient) (bool, error) {
	started, err := ensureControl(command.Context(), client)
	if err != nil {
		return false, err
	}
	result, err := setup.NewWizard(client).Ensure(command.Context(), command.InOrStdin(), command.OutOrStdout())
	if err != nil {
		if started {
			_ = process.StopDetached(config.ProjectName)
		}
		if errors.Is(err, setup.ErrCancelled) {
			_, _ = fmt.Fprintln(command.OutOrStdout(), "setup cancelled")
			return true, nil
		}
		return false, err
	}
	if result.Skipped {
		return false, nil
	}
	return false, restartControl(command.Context(), client)
}

func ensureControl(ctx context.Context, client controlClient) (bool, error) {
	status, err := process.StatusDetached(config.ProjectName)
	if err != nil {
		return false, err
	}
	started := false
	if !status.Running {
		if err := process.StartDetached(config.ProjectName, "serve"); err != nil {
			return false, err
		}
		started = true
	}
	return started, waitForControl(ctx, client)
}

func restartControl(ctx context.Context, client controlClient) error {
	if err := process.StopDetached(config.ProjectName); err != nil {
		return err
	}
	if err := process.StartDetached(config.ProjectName, "serve"); err != nil {
		return err
	}
	return waitForControl(ctx, client)
}

func waitForControl(ctx context.Context, client controlClient) error {
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := client.Health(waitCtx, &controlv1.HealthRequest{}); err == nil {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for control daemon: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

// redirectLogsToFile points the process-wide slog default at
// ${INFERENCERIG_HOME}/run/<name>.log instead of stderr, mirroring how a
// detached daemon's output is captured (platform/audit.AttachLogs).
func redirectLogsToFile(name string) error {
	path, err := audit.GetLogPath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	slog.SetDefault(logs.New(slog.LevelInfo, file))
	return nil
}

type controlClient interface {
	Health(context.Context, *controlv1.HealthRequest) (*controlv1.HealthResponse, error)
}
