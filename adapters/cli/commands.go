package cli

import (
	"connectrpc.com/connect"
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"inferencerig/config"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// dialTimeout bounds every control-socket dial the CLI makes. It must exceed
// the runtime supervisor's readiness timeout (core/runtime.defaultReadinessTimeout)
// with margin: a runtime-start call blocks server-side for up to that long, and
// a client timeout equal to or shorter than it races the server and reports a
// generic deadline-exceeded instead of the real readiness error.
const dialTimeout = 60 * time.Second

type dialer func(string, time.Duration) (controlv1connect.ControlServiceClient, error)
type call func(context.Context, controlv1connect.ControlServiceClient, []string) (proto.Message, error)

// Commands returns the full canonical-RPC-backed CLI.
func Commands(dial dialer) []*cobra.Command {
	if dial == nil {
		panic("cli: dial function is required")
	}
	return []*cobra.Command{
		healthCommand(dial), infoCommand(dial), profileCommand(dial), modelCommand(dial), backendCommand(dial),
		runtimeCommand(dial), signalsCommand(dial), eventsCommand(dial), configCommand(dial),
	}
}

func healthCommand(dial dialer) *cobra.Command {
	return rpcCommand("health", "Check daemon health", cobra.NoArgs, dial,
		func(ctx context.Context, client controlv1connect.ControlServiceClient, _ []string) (proto.Message, error) {
			return client.Health(ctx, &controlv1.HealthRequest{})
		})
}

func infoCommand(dial dialer) *cobra.Command {
	return rpcCommand("info", "Show daemon information", cobra.NoArgs, dial,
		func(ctx context.Context, client controlv1connect.ControlServiceClient, _ []string) (proto.Message, error) {
			return client.GetInfo(ctx, &controlv1.GetInfoRequest{})
		})
}

func profileCommand(dial dialer) *cobra.Command {
	group := &cobra.Command{Use: "profile", Short: "Manage canonical profiles"}
	group.AddCommand(
		rpcCommand("list", "List profiles", cobra.NoArgs, dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, _ []string) (proto.Message, error) {
			return client.ListProfiles(ctx, &controlv1.ListProfilesRequest{})
		}),
		rpcCommand("get <name>", "Get a profile", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.GetProfile(ctx, &controlv1.GetProfileRequest{Name: args[0]})
		}),
		profileWriteCommand("create <name> <yaml-file>", true, dial),
		profileWriteCommand("edit <name> <yaml-file>", false, dial),
		rpcCommand("delete <name>", "Delete a profile", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.DeleteProfile(ctx, &controlv1.DeleteProfileRequest{Name: args[0]})
		}),
		rpcCommand("cleanup <name>", "Delete a profile and its unshared local model", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.CleanupProfile(ctx, &controlv1.CleanupProfileRequest{Name: args[0]})
		}),
		rpcCommand("autostart <name> <true|false>", "Set profile autostart", cobra.ExactArgs(2), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			enabled, err := strconv.ParseBool(args[1])
			if err != nil {
				return nil, err
			}
			return client.SetProfileAutostart(ctx, &controlv1.SetProfileAutostartRequest{Name: args[0], Enabled: enabled})
		}),
	)
	return group
}

func profileWriteCommand(use string, createOnly bool, dial dialer) *cobra.Command {
	return rpcCommand(use, "Write a canonical YAML profile", cobra.ExactArgs(2), dial,
		func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			data, err := os.ReadFile(args[1])
			if err != nil {
				return nil, err
			}
			return client.PutProfile(ctx, &controlv1.PutProfileRequest{
				Name: args[0], ProfileYaml: string(data), CreateOnly: createOnly,
			})
		})
}

func modelCommand(dial dialer) *cobra.Command {
	group := &cobra.Command{Use: "model", Short: "Browse and manage models"}
	group.AddCommand(
		rpcCommand("search <backend> [query]", "Search the remote catalog", cobra.RangeArgs(1, 2), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			query := ""
			if len(args) == 2 {
				query = args[1]
			}
			return client.ListModelCatalog(ctx, &controlv1.ListModelCatalogRequest{Backend: args[0], Query: query})
		}),
		catalogWatchCommand(dial),
		rpcCommand("list <backend>", "List local models", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.ListLocalModels(ctx, &controlv1.ListLocalModelsRequest{Backend: args[0]})
		}),
		rpcCommand("resolve <profile>", "Resolve a profile model", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.ResolveProfileModel(ctx, &controlv1.ResolveProfileModelRequest{Profile: args[0]})
		}),
		rpcCommand("download <profile>", "Start a model download", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.StartModelDownload(ctx, &controlv1.StartModelDownloadRequest{Profile: args[0]})
		}),
		rpcCommand("get <id>", "Get download status", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.GetModelDownload(ctx, &controlv1.GetModelDownloadRequest{Id: args[0]})
		}),
		rpcCommand("cancel <id>", "Cancel a download", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.CancelModelDownload(ctx, &controlv1.CancelModelDownloadRequest{Id: args[0]})
		}),
		rpcCommand("apply <profile> <id>", "Apply a completed download", cobra.ExactArgs(2), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.ApplyDownloadToProfile(ctx, &controlv1.ApplyDownloadToProfileRequest{Profile: args[0], Id: args[1]})
		}),
		rpcCommand("rm <backend> <path>", "Delete a local model", cobra.ExactArgs(2), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.DeleteLocalModel(ctx, &controlv1.DeleteLocalModelRequest{Backend: args[0], Path: args[1]})
		}),
	)
	return group
}

func backendCommand(dial dialer) *cobra.Command {
	group := &cobra.Command{Use: "backend", Short: "Manage backends"}
	group.AddCommand(
		rpcCommand("list", "List backends", cobra.NoArgs, dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, _ []string) (proto.Message, error) {
			return client.ListBackends(ctx, &controlv1.ListBackendsRequest{})
		}),
		rpcCommand("status <backend>", "Show backend installation status", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.GetBackendInstallStatus(ctx, &controlv1.GetBackendInstallStatusRequest{Backend: args[0]})
		}),
		rpcCommand("install <backend> [version]", "Install a backend", cobra.RangeArgs(1, 2), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			version := ""
			if len(args) == 2 {
				version = args[1]
			}
			return client.InstallBackend(ctx, &controlv1.InstallBackendRequest{Backend: args[0], Version: version})
		}),
		rpcCommand("params <backend>", "List backend parameters", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.GetBackendParams(ctx, &controlv1.GetBackendParamsRequest{Backend: args[0]})
		}),
	)
	return group
}

func runtimeCommand(dial dialer) *cobra.Command {
	group := &cobra.Command{Use: "runtime", Short: "Manage profile runtimes"}
	group.AddCommand(
		rpcCommand("status <profile>", "Show runtime status", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.GetRuntimeStatus(ctx, &controlv1.GetRuntimeStatusRequest{Profile: args[0]})
		}),
		rpcCommand("start <profile>", "Start a runtime", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.StartRuntime(ctx, &controlv1.StartRuntimeRequest{Profile: args[0]})
		}),
		rpcCommand("stop <profile>", "Stop a runtime", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.StopRuntime(ctx, &controlv1.StopRuntimeRequest{Profile: args[0]})
		}),
		rpcCommand("restart <profile>", "Restart a runtime", cobra.ExactArgs(1), dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.RestartRuntime(ctx, &controlv1.RestartRuntimeRequest{Profile: args[0]})
		}),
	)
	return group
}

func signalsCommand(dial dialer) *cobra.Command {
	return rpcCommand("signals", "Show host signals", cobra.NoArgs, dial,
		func(ctx context.Context, client controlv1connect.ControlServiceClient, _ []string) (proto.Message, error) {
			return client.GetSignals(ctx, &controlv1.GetSignalsRequest{})
		})
}

func eventsCommand(dial dialer) *cobra.Command {
	group := &cobra.Command{Use: "events", Short: "Inspect control events"}
	group.AddCommand(
		rpcCommand("list", "List events", cobra.NoArgs, dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, _ []string) (proto.Message, error) {
			return client.ListEvents(ctx, &controlv1.ListEventsRequest{})
		}),
		streamCommand("watch", "Watch events", dial, watchEvents),
	)
	return group
}

func configCommand(dial dialer) *cobra.Command {
	group := &cobra.Command{Use: "config", Short: "Manage daemon configuration"}
	group.AddCommand(rpcCommand("startup [service...]", "Set startup services", cobra.ArbitraryArgs, dial,
		func(ctx context.Context, client controlv1connect.ControlServiceClient, args []string) (proto.Message, error) {
			return client.SetStartupServices(ctx, &controlv1.SetStartupServicesRequest{Services: args})
		}))
	return group
}

func rpcCommand(use, short string, args cobra.PositionalArgs, dial dialer, invoke call) *cobra.Command {
	var socket string
	command := &cobra.Command{
		Use: use, Short: short, Args: args, ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(command *cobra.Command, values []string) error {
			client, err := dial(resolveSocket(socket), dialTimeout)
			if err != nil {
				return err
			}
			response, err := invoke(command.Context(), client, values)
			if err != nil {
				return err
			}
			return printProto(command, response)
		},
	}
	command.Flags().StringVar(&socket, "socket", "", "control Unix socket")
	return command
}

type streamCall func(context.Context, controlv1connect.ControlServiceClient, func(proto.Message) error) error

func streamCommand(use, short string, dial dialer, invoke streamCall) *cobra.Command {
	var socket string
	command := &cobra.Command{
		Use: use, Short: short, Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			client, err := dial(resolveSocket(socket), dialTimeout)
			if err != nil {
				return err
			}
			return invoke(command.Context(), client, func(message proto.Message) error {
				return printProto(command, message)
			})
		},
	}
	command.Flags().StringVar(&socket, "socket", "", "control Unix socket")
	return command
}

func catalogWatchCommand(dial dialer) *cobra.Command {
	return streamCommand("watch", "Watch catalog refreshes", dial, func(ctx context.Context, client controlv1connect.ControlServiceClient, print func(proto.Message) error) error {
		stream, err := client.WatchModelCatalog(ctx, &controlv1.WatchModelCatalogRequest{})
		if err != nil {
			return err
		}
		return drainStream(stream, print)
	})
}

// drainStream prints every message then reports the stream's terminal error.
// That last step is the one most easily dropped when a watch is added, and
// dropping it turns a broken stream into a clean end of output.
func drainStream[T any, PT interface {
	*T
	proto.Message
}](stream *connect.ServerStreamForClient[T], print func(proto.Message) error) error {
	for stream.Receive() {
		if err := print(PT(stream.Msg())); err != nil {
			return err
		}
	}
	return stream.Err()
}

func watchEvents(ctx context.Context, client controlv1connect.ControlServiceClient, print func(proto.Message) error) error {
	stream, err := client.WatchEvents(ctx, &controlv1.WatchEventsRequest{})
	if err != nil {
		return err
	}
	return drainStream(stream, print)
}

func resolveSocket(flag string) string {
	if flag != "" {
		return flag
	}
	return os.Getenv(config.ProjectSocketEnv)
}

func printProto(command *cobra.Command, message proto.Message) error {
	data, err := protojson.MarshalOptions{Indent: "  "}.Marshal(message)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(command.OutOrStdout(), string(data))
	return err
}
