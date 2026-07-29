package rpc

import (
	"context"
	"net"
	"net/http"
	"time"

	"inferencerig/config"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// ControlTransport returns an HTTP transport that dials the local control Unix
// socket. An empty socketPath resolves to config.ControlSocketPath().
func ControlTransport(socketPath string) (*http.Transport, error) {
	if socketPath == "" {
		path, err := config.ControlSocketPath()
		if err != nil {
			return nil, err
		}
		socketPath = path
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socketPath)
		},
		DisableKeepAlives: true,
	}, nil
}

// DialControl creates a canonical control client over the local Unix socket.
func DialControl(socketPath string, timeout time.Duration) (controlv1connect.ControlServiceClient, error) {
	transport, err := ControlTransport(socketPath)
	if err != nil {
		return nil, err
	}
	return controlv1connect.NewControlServiceClient(
		&http.Client{Transport: transport, Timeout: timeout},
		"http://unix",
	), nil
}
