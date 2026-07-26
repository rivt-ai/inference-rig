package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"inferencerig/config"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

type call func(context.Context, controlv1connect.ControlServiceClient, string) (proto.Message, error)

type commandSpec struct {
	use   string
	short string
	args  cobra.PositionalArgs
	call  call
}

// Commands returns the canonical-RPC-backed user commands.
func Commands(dial func(string, time.Duration) (controlv1connect.ControlServiceClient, error)) []*cobra.Command {
	if dial == nil {
		panic("cli: dial function is required")
	}
	specs := []commandSpec{
		{"backends", "List available backends", cobra.NoArgs, listBackends},
		{"profiles", "List canonical profiles", cobra.NoArgs, listProfiles},
		{"status <profile>", "Show runtime status", cobra.ExactArgs(1), runtimeStatus},
		{"start <profile>", "Start a profile", cobra.ExactArgs(1), startRuntime},
		{"stop <profile>", "Stop a profile", cobra.ExactArgs(1), stopRuntime},
	}
	out := make([]*cobra.Command, 0, len(specs))
	for _, spec := range specs {
		out = append(out, newCommand(spec, dial))
	}
	return out
}

func newCommand(spec commandSpec, dial func(string, time.Duration) (controlv1connect.ControlServiceClient, error)) *cobra.Command {
	var socket string
	command := &cobra.Command{
		Use: spec.use, Short: spec.short, Args: spec.args,
		RunE: func(command *cobra.Command, args []string) error {
			path := socket
			if path == "" {
				path = os.Getenv(config.ProjectSocketEnv)
			}
			client, err := dial(path, 30*time.Second)
			if err != nil {
				return err
			}
			arg := ""
			if len(args) > 0 {
				arg = args[0]
			}
			response, err := spec.call(command.Context(), client, arg)
			if err != nil {
				return err
			}
			data, err := protojson.MarshalOptions{Indent: "  "}.Marshal(response)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), string(data))
			return err
		},
	}
	command.Flags().StringVar(&socket, "socket", "", "control Unix socket")
	return command
}

func listBackends(ctx context.Context, client controlv1connect.ControlServiceClient, _ string) (proto.Message, error) {
	return client.ListBackends(ctx, &controlv1.ListBackendsRequest{})
}

func listProfiles(ctx context.Context, client controlv1connect.ControlServiceClient, _ string) (proto.Message, error) {
	return client.ListProfiles(ctx, &controlv1.ListProfilesRequest{})
}

func runtimeStatus(ctx context.Context, client controlv1connect.ControlServiceClient, profile string) (proto.Message, error) {
	return client.GetRuntimeStatus(ctx, &controlv1.GetRuntimeStatusRequest{Profile: profile})
}

func startRuntime(ctx context.Context, client controlv1connect.ControlServiceClient, profile string) (proto.Message, error) {
	return client.StartRuntime(ctx, &controlv1.StartRuntimeRequest{Profile: profile})
}

func stopRuntime(ctx context.Context, client controlv1connect.ControlServiceClient, profile string) (proto.Message, error) {
	return client.StopRuntime(ctx, &controlv1.StopRuntimeRequest{Profile: profile})
}
