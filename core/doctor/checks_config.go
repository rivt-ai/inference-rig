package doctor

import (
	"context"
	"errors"
	"net"

	"inferencerig/config"
)

// Remedy IDs. They are the stable handle a caller names a fix by, so they are
// part of the JSON contract and must not be reworded casually.
const (
	RemedyBindLoopback   = "bind-loopback"
	RemedyRequireAuth    = "require-auth"
	RemedyAllowExposed   = "allow-exposed"
	loopbackHost         = "127.0.0.1"
	exposedWithoutAuthID = "config.valid"
)

// checkConfigValid asks the same question startup asks, by running the same
// validator. Anything less risks a doctor that passes a config the daemon then
// refuses, which is worse than no check at all.
func checkConfigValid(ctx context.Context, e *env) Check {
	const title = "configuration"
	if e.opts.ValidateConfig == nil {
		return skip(exposedWithoutAuthID, title, "no validator wired")
	}
	err := e.opts.ValidateConfig(ctx)
	switch {
	case err == nil:
		return ok(exposedWithoutAuthID, title, "valid")
	case errors.Is(err, config.ErrExposedWithoutAuth):
		// The one failure with known, named ways out.
		return fail(exposedWithoutAuthID, title, "authentication is disabled on a bind that reaches the network").
			withDetail(err.Error()).
			withRemedies(exposureRemedies(e.listenAddr())...)
	default:
		return fail(exposedWithoutAuthID, title, "startup would reject this configuration").
			withDetail(err.Error())
	}
}

// exposureRemedies lists all three legal ways out, in increasing order of
// exposure. Which one is right depends on what the operator is deploying, so
// doctor states the options and the trade-off rather than choosing.
func exposureRemedies(listenAddr string) []Remedy {
	return []Remedy{
		{
			ID:         RemedyBindLoopback,
			Title:      "bind loopback — only this machine can reach the daemon",
			ConfigEdit: "listen_addr: " + loopbackAddr(listenAddr),
		},
		{
			ID:         RemedyRequireAuth,
			Title:      "keep the network bind, require a token",
			ConfigEdit: "security:\n  disable_auth: false",
		},
		{
			ID:    RemedyAllowExposed,
			Title: "keep the network bind unauthenticated — every RPC is public to anything that can route to the port",
			ConfigEdit: "security:\n  disable_auth: true\n" +
				"  allow_exposed_without_auth: true",
		},
	}
}

// loopbackAddr keeps the configured port while moving the host to loopback.
func loopbackAddr(listenAddr string) string {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "" {
		return config.DefaultListenAddr
	}
	return net.JoinHostPort(loopbackHost, port)
}

// checkAuthPosture reports a bind that is exposed and unauthenticated but has
// been opted into. That config loads by design, so it is not a failure — but it
// is the single most consequential setting here and should never be silent.
//
// It is derived from config rather than the gateway's /health, which only
// reports posture over HTTP and so is unavailable exactly when the daemon is
// down and doctor is most needed.
func checkAuthPosture(_ context.Context, e *env) Check {
	const id, title = "gateway.auth_posture", "gateway authentication"
	if e.loadErr != nil {
		return skip(id, title, "configuration could not be loaded")
	}
	exposed := e.cfg.AllowsNonLoopback()
	switch {
	case !e.cfg.Security.DisableAuth:
		return ok(id, title, "every RPC requires a token")
	case !exposed:
		return ok(id, title, "authentication disabled on a loopback bind")
	default:
		return warn(id, title, "serving unauthenticated on "+e.cfg.ListenAddr).
			withDetail("Every RPC is reachable without a token by anything that can route to this port, " +
				"including runtime start and stop. This is allowed only because " +
				"security.allow_exposed_without_auth is set.").
			withRemedies(exposureRemedies(e.cfg.ListenAddr)[:2]...)
	}
}
