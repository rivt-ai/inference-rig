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
			server := &http.Server{
				Addr: cfg.ListenAddr,
				Handler: public_http.NewHandler(public_http.Dependencies{
					Control: client, AuthToken: os.Getenv(cfg.Security.AuthTokenEnv), AppFS: app,
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
