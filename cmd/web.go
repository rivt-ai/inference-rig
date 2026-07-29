package cmd

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"inferencerig/adapters/public_http"
	"inferencerig/config"
	"inferencerig/core/rpc"
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
				// Load rejects this alongside a non-loopback bind, so reaching
				// here means the gateway is local-only and the operator asked
				// for it. Say so once: an open gateway must never be silent.
				command.Printf("security.disable_auth is set; serving %s without authentication\n\n", cfg.ListenAddr)
			} else {
				generated := false
				token, generated = public_http.ResolveAuthToken(os.Getenv(cfg.Security.AuthTokenEnv))
				if generated {
					command.Printf("no %s set; generated a gateway token for this run:\n\n    %s\n\n",
						cfg.Security.AuthTokenEnv, token)
				}
			}
			server := &http.Server{
				Addr: cfg.ListenAddr,
				Handler: public_http.NewHandler(public_http.Dependencies{
					Control:            client,
					AuthToken:          token,
					DisableAuth:        cfg.Security.DisableAuth,
					AppFS:              app,
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
