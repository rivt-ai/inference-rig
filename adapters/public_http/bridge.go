package public_http

import (
	"context"

	"connectrpc.com/connect"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// controlBridge adapts the control *client* into a control *handler* so the
// gateway can re-export the canonical service to browsers.
//
// The generated unary client and handler interfaces are identical, so embedding
// satisfies all of them; only the two server streams need real work, because a
// client receives a stream where a handler sends one.
type controlBridge struct {
	controlv1connect.ControlServiceClient
}

var _ controlv1connect.ControlServiceHandler = controlBridge{}

func (b controlBridge) WatchEvents(ctx context.Context, request *controlv1.WatchEventsRequest, stream *connect.ServerStream[controlv1.WatchEventsResponse]) error {
	return forward(b.ControlServiceClient.WatchEvents(ctx, request))(stream)
}

func (b controlBridge) WatchModelCatalog(ctx context.Context, request *controlv1.WatchModelCatalogRequest, stream *connect.ServerStream[controlv1.WatchModelCatalogResponse]) error {
	return forward(b.ControlServiceClient.WatchModelCatalog(ctx, request))(stream)
}

func (b controlBridge) WatchLogs(ctx context.Context, request *controlv1.WatchLogsRequest, stream *connect.ServerStream[controlv1.WatchLogsResponse]) error {
	return forward(b.ControlServiceClient.WatchLogs(ctx, request))(stream)
}

// forward carries the dial error a client stream method returns alongside the
// stream itself, so each bridge method is one line: open upstream, copy it
// down. A failed dial is reported and nothing is copied.
func forward[T any](upstream *connect.ServerStreamForClient[T], err error) func(*connect.ServerStream[T]) error {
	return func(downstream *connect.ServerStream[T]) error {
		if err != nil {
			return err
		}
		return pipeStream(upstream, downstream)
	}
}

// pipeStream copies an upstream server stream to the downstream one until the
// upstream ends or either side fails. Closing the upstream is what releases the
// control-socket connection, so it happens on every exit path.
func pipeStream[T any](upstream *connect.ServerStreamForClient[T], downstream *connect.ServerStream[T]) error {
	defer func() { _ = upstream.Close() }()
	for upstream.Receive() {
		if err := downstream.Send(upstream.Msg()); err != nil {
			return err
		}
	}
	return upstream.Err()
}
