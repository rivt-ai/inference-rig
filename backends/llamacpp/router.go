package llamacpp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"inferencerig/core/runtime"
)

// Model is one model the router reports, with its load status.
type Model struct {
	ID     string `json:"id"`
	Status struct {
		Value string `json:"value"`
	} `json:"status"`
}

// Client talks to a running llama-server router's model-management API.
// Ported and neutralized from llamarig core/router/client.go.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a router client for baseURL. A nil http client gets a
// default with a request timeout.
func NewClient(baseURL string, client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: client}
}

// List returns the models the router currently knows.
func (c *Client) List(ctx context.Context) ([]Model, error) { return c.list(ctx, "/models") }

// Reload re-reads the router's models source (the generated models.ini).
func (c *Client) Reload(ctx context.Context) ([]Model, error) { return c.list(ctx, "/models?reload=1") }

// Load asks the router to load a model by ID.
func (c *Client) Load(ctx context.Context, model string) error {
	return c.do(ctx, http.MethodPost, "/models/load", map[string]string{"model": model}, nil)
}

// Unload asks the router to unload a model by ID.
func (c *Client) Unload(ctx context.Context, model string) error {
	return c.do(ctx, http.MethodPost, "/models/unload", map[string]string{"model": model}, nil)
}

func (c *Client) list(ctx context.Context, path string) ([]Model, error) {
	var response struct {
		Data []Model `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (c *Client) do(ctx context.Context, method, path string, body map[string]string, dst any) error {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("router %s %s: %w", method, path, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.statusError(method, path, resp)
	}
	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("decode router %s: %w", path, err)
		}
	}
	return nil
}

func (c *Client) statusError(method, path string, resp *http.Response) error {
	detail := ""
	if data, readErr := io.ReadAll(io.LimitReader(resp.Body, 512)); readErr == nil && len(data) > 0 {
		detail = ": " + strings.TrimSpace(string(data))
	}
	return fmt.Errorf("router %s %s returned %s%s", method, path, resp.Status, detail)
}

// Controller ties the generic supervisor (router process lifecycle) to the
// router client (model management within it). The router process is supervised
// through the neutral LaunchSpec; the client drives models once it is up. This
// is the llama.cpp runtime control beyond the generic supervisor.
type Controller struct {
	sup    *runtime.Supervisor
	client *Client
}

// NewController builds a controller supervising the router described by spec and
// managing its models over baseURL. A nil http client uses the default.
func NewController(spec runtime.LaunchSpec, baseURL string, httpClient *http.Client) *Controller {
	return &Controller{sup: runtime.NewSupervisor(spec), client: NewClient(baseURL, httpClient)}
}

// Client exposes the router model-management client.
func (c *Controller) Client() *Client { return c.client }

// Start launches the router process and waits for readiness.
func (c *Controller) Start(ctx context.Context) (runtime.CommandResult, error) {
	return c.sup.Start(ctx)
}

// Stop gracefully stops the router process.
func (c *Controller) Stop(ctx context.Context) (runtime.CommandResult, error) {
	return c.sup.Stop(ctx)
}

// Recover adopts an already-running router recorded in the PID file.
func (c *Controller) Recover(ctx context.Context) (bool, error) { return c.sup.Recover(ctx) }

// Status reports the supervised router's state.
func (c *Controller) Status(ctx context.Context) (runtime.Status, error) {
	return c.sup.Status(ctx)
}

// Reload refreshes the router's models source after a regenerated models.ini.
func (c *Controller) Reload(ctx context.Context) ([]Model, error) { return c.client.Reload(ctx) }
