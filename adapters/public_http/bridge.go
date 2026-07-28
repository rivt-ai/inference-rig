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

func (b controlBridge) WatchEvents(
	ctx context.Context,
	request *controlv1.WatchEventsRequest,
	stream *connect.ServerStream[controlv1.WatchEventsResponse],
) error {
	upstream, err := b.ControlServiceClient.WatchEvents(ctx, request)
	if err != nil {
		return err
	}
	return pipeStream(upstream, stream)
}

func (b controlBridge) WatchModelCatalog(
	ctx context.Context,
	request *controlv1.WatchModelCatalogRequest,
	stream *connect.ServerStream[controlv1.WatchModelCatalogResponse],
) error {
	upstream, err := b.ControlServiceClient.WatchModelCatalog(ctx, request)
	if err != nil {
		return err
	}
	return pipeStream(upstream, stream)
}

func (b controlBridge) WatchLogs(
	ctx context.Context,
	request *controlv1.WatchLogsRequest,
	stream *connect.ServerStream[controlv1.WatchLogsResponse],
) error {
	upstream, err := b.ControlServiceClient.WatchLogs(ctx, request)
	if err != nil {
		return err
	}
	return pipeStream(upstream, stream)
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
