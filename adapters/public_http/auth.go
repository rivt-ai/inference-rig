package public_http

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ResolveAuthToken returns the gateway token, in the order env, file, generate.
// An unset token is generated rather than treated as "no auth": a blank token
// used to disable the guard entirely, which quietly left every route open on a
// machine the user believed was protected.
//
// A generated token is written to path (0600) and reused on the next run.
// Every RPC is authenticated, so a per-run token would mean a blank dashboard
// after every restart. An empty path skips persistence, which is what the tests
// and any caller without a resolvable home directory get.
func ResolveAuthToken(configured, path string) (token string, generated bool) {
	if configured != "" {
		return configured, false
	}
	if stored, err := os.ReadFile(path); err == nil {
		if trimmed := strings.TrimSpace(string(stored)); trimmed != "" {
			return trimmed, false
		}
	}
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		// crypto/rand cannot fail on any supported platform; if it somehow
		// does, refusing to serve beats serving without a token.
		panic("public_http: cannot generate auth token: " + err.Error())
	}
	token = hex.EncodeToString(buffer)
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
			// A token that cannot be persisted still authenticates this run;
			// the caller prints it, so the operator is not locked out.
			_ = os.WriteFile(path, []byte(token), 0o600)
		}
	}
	return token, true
}

// requireToken authenticates a route. There is no read exemption anywhere it is
// applied: reads expose profiles, installed models, runtime state, telemetry,
// logs and audit records, which is the whole state of the machine.
func requireToken(token string, disabled bool, next http.Handler) http.Handler {
	if disabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !tokenMatches(token, r.Header.Get("Authorization")) {
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func tokenMatches(token, header string) bool {
	presented := strings.TrimPrefix(header, "Bearer ")
	return subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1
}

// originGuard rejects cross-origin browser requests. Authentication is header-
// based and there are no cookies, so a malicious page holds no ambient
// credential to spend and the origin guard is not the primary defence it once
// was. It stays because it is what keeps *insecure mode* — where there is no
// credential to withhold — from being reachable by DNS rebinding.
//
// A missing Origin header means a non-browser caller (curl, the CLI, the TUI)
// and is allowed: those clients are not subject to the browser's ambient
// credential model.
func originGuard(deps Dependencies, next http.Handler) http.Handler {
	if deps.DisableOriginCheck {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || originAllowed(origin, deps.AllowedOrigins) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "origin not allowed", http.StatusForbidden)
	})
}

func originAllowed(origin string, allowed []string) bool {
	if len(allowed) > 0 {
		return slices.Contains(allowed, origin)
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
