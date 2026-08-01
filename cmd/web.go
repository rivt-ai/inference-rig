package cmd

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"inferencerig/adapters/public_http"
	"inferencerig/config"
	"inferencerig/core/rpc"
	"inferencerig/platform/pidfile"
	"inferencerig/webui"
)

func webCommand() *cobra.Command {
	return &cobra.Command{
		Use: "web", Short: "Serve the browser, REST, and MCP gateway", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			client, err := rpc.DialControl("", 30*time.Second)
			if err != nil {
				return err
			}
			removePIDFile, err := registerWebPID()
			if err != nil {
				return err
			}
			defer removePIDFile()
			cfg := config.Default()
			if loaded, err := config.Load(); err == nil {
				cfg = loaded
			}
			app, err := fs.Sub(webui.Files, "dist")
			if err != nil {
				return err
			}
			var token string
			if cfg.Security.DisableAuth {
				// An open gateway must never be silent. A non-loopback bind is
				// refused by config.ValidateSecurity unless deliberately opted
				// into, and warns via config.WarnIfExposed when it is.
				command.Printf("security.disable_auth is set; serving %s without authentication\n"+
					"Every profile, model, log and audit record is readable, and every runtime\n"+
					"operation is executable, by anything that can reach that address.\n\n", cfg.ListenAddr)
			} else {
				tokenPath, _ := config.GatewayTokenPath()
				generated := false
				token, generated = public_http.ResolveAuthToken(os.Getenv(cfg.Security.AuthTokenEnv), tokenPath)
				if generated {
					command.Printf("no %s set; generated a gateway token and saved it to %s\n",
						cfg.Security.AuthTokenEnv, tokenPath)
				}
				// The token rides in the fragment, not the query: a fragment is
				// never sent to the server, so it cannot land in an access log.
				command.Printf("Open the web UI:\n\n    %s/#token=%s\n\n", config.BrowseURL(cfg.ListenAddr), token)
			}
			server := &http.Server{
				Addr: cfg.ListenAddr,
				Handler: public_http.NewHandler(public_http.Dependencies{
					Control:            client,
					AuthToken:          token,
					DisableAuth:        cfg.Security.DisableAuth,
					AppFS:              app,
					AllowedOrigins:     cfg.Security.AllowedOrigins,
					DisableOriginCheck: cfg.Security.DisableOriginCheck,
				}),
				ReadHeaderTimeout: 5 * time.Second,
			}
			go func() {
				<-command.Context().Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(ctx)
			}()
			err = server.ListenAndServe()
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		},
	}
}

// registerWebPID self-registers this process's PID like the control
// daemon's own PID file (bootstrap.Service): written here regardless of how
// this process was launched, so the TUI's "Stop" can find and stop a gateway
// it did not itself start (a bare CLI invocation, a unit file, ...).
func registerWebPID() (func(), error) {
	home, err := config.Home()
	if err != nil {
		return nil, err
	}
	file := pidfile.New(filepath.Join(home, "run", config.StartupServiceWeb+".pid"))
	pid := os.Getpid()
	if err := file.Write(pid); err != nil {
		return nil, err
	}
	return func() { _ = file.Remove(pid) }, nil
}
