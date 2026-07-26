package cmd

import (
	"context"
	"errors"
	"fmt"
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
		if err := runFirstSetup(command, client); err != nil {
			return err
		}
	}
	return adaptertui.RunInteractive(command.Context(), command.InOrStdin(), command.OutOrStdout(), adaptertui.Options{
		Client: client, ManageLocalServices: local,
	})
}

func runFirstSetup(command *cobra.Command, client controlv1connect.ControlServiceClient) error {
	empty, err := profilesEmpty()
	if err != nil || !empty {
		return err
	}
	if err := ensureControl(command.Context(), client); err != nil {
		return err
	}
	profile, err := setup.NewWizard(client).RunInteractive(command.Context(), command.InOrStdin(), command.OutOrStdout())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(command.OutOrStdout(), "created profile %s\n", profile.GetName())
	return err
}

func profilesEmpty() (bool, error) {
	root, err := config.ProfilesDir()
	if err != nil {
		return false, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if info, err := os.Stat(filepath.Join(root, entry.Name(), "profile.yaml")); err == nil && info.Mode().IsRegular() {
				return false, nil
			}
		}
	}
	return true, nil
}

func ensureControl(ctx context.Context, client controlClient) error {
	status, err := process.StatusDetached(config.ProjectName)
	if err != nil {
		return err
	}
	if !status.Running {
		if err := process.StartDetached(config.ProjectName, "serve"); err != nil {
			return err
		}
	}
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

type controlClient interface {
	Health(context.Context, *controlv1.HealthRequest) (*controlv1.HealthResponse, error)
}
