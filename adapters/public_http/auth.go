package public_http

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"connectrpc.com/connect"

	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// mutatingProcedures is the set of procedures that change state and therefore
// require the auth token. Reads are open so the UI can render before the user
// has pasted a token, matching how the gateway behaved before.
//
// A new mutating RPC must be added here. The test in server_test.go walks the
// service descriptor and fails if a procedure is in neither this set nor the
// documented read set, so the omission cannot pass review silently.
var mutatingProcedures = map[string]struct{}{
	controlv1connect.ControlServicePutProfileProcedure:             {},
	controlv1connect.ControlServiceDeleteProfileProcedure:          {},
	controlv1connect.ControlServiceCleanupProfileProcedure:         {},
	controlv1connect.ControlServiceSetProfileAutostartProcedure:    {},
	controlv1connect.ControlServiceSetStartupServicesProcedure:     {},
	controlv1connect.ControlServiceInstallBackendProcedure:         {},
	controlv1connect.ControlServiceStartRuntimeProcedure:           {},
	controlv1connect.ControlServiceStopRuntimeProcedure:            {},
	controlv1connect.ControlServiceRestartRuntimeProcedure:         {},
	controlv1connect.ControlServiceStartModelDownloadProcedure:     {},
	controlv1connect.ControlServiceCancelModelDownloadProcedure:    {},
	controlv1connect.ControlServiceApplyDownloadToProfileProcedure: {},
	controlv1connect.ControlServiceDeleteLocalModelProcedure:       {},
	controlv1connect.ControlServiceDeleteLogArchiveProcedure:       {},
	controlv1connect.ControlServiceClearLogArchivesProcedure:       {},
}

// ResolveAuthToken returns the gateway token. An unset token is generated
// rather than treated as "no auth": a blank token used to disable the guard
// entirely, which quietly left every mutating route open on a machine the user
// believed was protected. The caller is expected to log a generated token once
// so the operator can use it.
func ResolveAuthToken(configured string) (token string, generated bool) {
	if configured != "" {
		return configured, false
	}
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		// crypto/rand cannot fail on any supported platform; if it somehow
		// does, refusing to serve beats serving without a token.
		panic("public_http: cannot generate auth token: " + err.Error())
	}
	return hex.EncodeToString(buffer), true
}

func connectInterceptors(token string) connect.HandlerOption {
	return connect.WithInterceptors(connect.UnaryInterceptorFunc(
		func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
				if _, mutating := mutatingProcedures[request.Spec().Procedure]; !mutating {
					return next(ctx, request)
				}
				if !tokenMatches(token, request.Header().Get("Authorization")) {
					return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authorization required"))
				}
				return next(ctx, request)
			}
		},
	))
}

func requireToken(token string, next http.Handler) http.Handler {
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

// originGuard rejects cross-origin browser requests. The gateway binds loopback
// and holds a token that any page in the user's browser could otherwise spend
// via DNS rebinding, so an Origin that is present and not permitted is refused
// before it reaches a handler.
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
		if origin == "" || originAllowed(origin, deps.AllowedOrigin) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "origin not allowed", http.StatusForbidden)
	})
}

func originAllowed(origin, allowed string) bool {
	if allowed != "" {
		return origin == allowed
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
