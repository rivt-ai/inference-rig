package llamacpp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"inferencerig/core/profiles"
)

// profileFor points a profile at addr, which is how the activator learns where
// the router it should talk to is listening.
func profileFor(t *testing.T, name, addr string) profiles.Profile {
	t.Helper()
	parsed, err := url.Parse(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return profiles.Profile{Name: name, Listen: profiles.ListenSpec{Host: parsed.Hostname(), Port: port}}
}

func TestActivateRuntimeLoadsTheProfilePreset(t *testing.T) {
	var gotPath, gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body["model"]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	backend := New(Options{})
	if err := backend.ActivateRuntime(context.Background(), profileFor(t, "qwen-copy", server.URL)); err != nil {
		t.Fatalf("ActivateRuntime: %v", err)
	}
	if gotPath != "/models/load" {
		t.Fatalf("path = %q, want /models/load", gotPath)
	}
	// The router keys presets by profile name, because that is the section name
	// written into the generated models.ini.
	if gotModel != "qwen-copy" {
		t.Fatalf("model = %q, want qwen-copy", gotModel)
	}
}

// Starting a profile that the router already serves is the desired end state,
// so the router's "already running" rejection must not surface as a failure.
func TestActivateRuntimeTreatsAlreadyRunningAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"model is already running","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	backend := New(Options{})
	if err := backend.ActivateRuntime(context.Background(), profileFor(t, "qwen", server.URL)); err != nil {
		t.Fatalf("already-running reported as error: %v", err)
	}
}

func TestActivateRuntimeReportsTheRouterMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"model not found","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	backend := New(Options{})
	err := backend.ActivateRuntime(context.Background(), profileFor(t, "missing", server.URL))
	if err == nil {
		t.Fatal("ActivateRuntime succeeded against a router that rejected the preset")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("error = %v, want the router's message", err)
	}
}

// A wildcard bind is reached over loopback, matching how readiness probes it.
func TestActivateHostResolvesWildcardBinds(t *testing.T) {
	for _, host := range []string{"", "0.0.0.0", "::"} {
		if got := activateHost(host); got != "127.0.0.1" {
			t.Fatalf("activateHost(%q) = %q, want 127.0.0.1", host, got)
		}
	}
	if got := activateHost("192.168.1.5"); got != "192.168.1.5" {
		t.Fatalf("activateHost kept host = %q", got)
	}
}
