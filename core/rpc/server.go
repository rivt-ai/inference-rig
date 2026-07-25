package rpc

import (
	"net"
	"net/http"
	"time"
)

// Server bundles the control-plane HTTP server with the Unix-socket listener it
// serves on. Callers run Server.HTTP.Serve(Server.Listener).
type Server struct {
	HTTP       *http.Server
	Listener   net.Listener
	SocketPath string
}

// NewServer creates a control RPC server listening on the local Unix socket,
// serving handler mounted at path. handler is any http.Handler (e.g. a
// generated Connect service handler), keeping this wrapper decoupled from the
// canonical proto service.
func NewServer(path string, handler http.Handler) (Server, error) {
	listener, socketPath, err := NewControlListener()
	if err != nil {
		return Server{}, err
	}
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	return Server{
		HTTP: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		Listener:   listener,
		SocketPath: socketPath,
	}, nil
}
