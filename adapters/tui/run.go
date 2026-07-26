package tui

import (
	"context"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"

	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// Options supplies the canonical client and local-process policy.
type Options struct {
	Client              controlv1connect.ControlServiceClient
	ManageLocalServices bool
}

// RunInteractive starts the full-screen canonical control dashboard.
func RunInteractive(ctx context.Context, input io.Reader, output io.Writer, options Options) error {
	if options.Client == nil {
		return fmt.Errorf("tui: control client is required")
	}
	_, err := tea.NewProgram(
		newModel(ctx, options.Client, options.ManageLocalServices),
		tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output),
	).Run()
	return err
}
