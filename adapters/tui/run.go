package tui

import (
	"context"
	"fmt"
	"io"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// Run renders a compact terminal dashboard from canonical RPC snapshots.
func Run(ctx context.Context, out io.Writer, client controlv1connect.ControlServiceClient, profile string) error {
	if client == nil {
		return fmt.Errorf("tui: control client is required")
	}
	backends, err := client.ListBackends(ctx, &controlv1.ListBackendsRequest{})
	if err != nil {
		return err
	}
	profiles, err := client.ListProfiles(ctx, &controlv1.ListProfilesRequest{})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "InferenceRig\nBackends: %d\nProfiles: %d\n", len(backends.GetBackends()), len(profiles.GetProfiles())); err != nil {
		return err
	}
	if profile == "" {
		return nil
	}
	status, err := client.GetRuntimeStatus(ctx, &controlv1.GetRuntimeStatusRequest{Profile: profile})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Runtime %s: %s\n", profile, status.GetStatus().GetState())
	return err
}
