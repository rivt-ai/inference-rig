package llamacpp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"inferencerig/core/profiles"
)

// loadPath is the router endpoint that selects which preset to load. The router
// answers it immediately and loads in the background, so activation costs a
// round trip rather than the model-load latency.
const loadPath = "/models/load"

// activateTimeout bounds only the request that asks the router to start
// loading, never the load itself.
const activateTimeout = 5 * time.Second

// alreadyRunning is the router's reply when the requested preset is already the
// loaded one. That is the desired end state, so it is not an error here.
const alreadyRunning = "already running"

// ActivateRuntime tells the router to load this profile's preset. Without it
// the router stays idle after start and loads on the first request instead,
// which is what made a started profile look like it was not running.
//
// The preset is keyed by profile name because that is the section name written
// into the generated models.ini, so the router's model id and the profile name
// are the same string by construction.
func (b *Backend) ActivateRuntime(ctx context.Context, p profiles.Profile) error {
	body, err := json.Marshal(map[string]string{"model": p.Name})
	if err != nil {
		return err
	}
	endpoint := "http://" + net.JoinHostPort(activateHost(p.Listen.Host), strconv.Itoa(p.Listen.Port)) + loadPath
	ctx, cancel := context.WithTimeout(ctx, activateTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 300 {
		return nil
	}
	message := errorMessage(response)
	if bytes.Contains([]byte(message), []byte(alreadyRunning)) {
		return nil
	}
	return fmt.Errorf("load %q: %s", p.Name, message)
}

// errorMessage extracts the router's error text, falling back to the status
// line when the body is not the shape we expect.
func errorMessage(response *http.Response) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err == nil && payload.Error.Message != "" {
		return payload.Error.Message
	}
	return response.Status
}

// activateHost keeps a wildcard bind addressable: the router listens on every
// interface but is reached over loopback, matching how readiness probes it.
func activateHost(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}
