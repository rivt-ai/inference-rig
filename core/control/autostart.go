package control

import (
	"context"
	"fmt"
	"net"
	"slices"
	"sort"
	"strconv"
	"time"

	coreruntime "inferencerig/core/runtime"
)

const (
	autostartAttempts = 3
	autostartBackoff  = 250 * time.Millisecond
)

// ValidateAutostart rejects profile sets the runtime slot can never serve.
func (m *Manager) ValidateAutostart(ctx context.Context, names []string) error {
	var backend, address string
	var singleActive bool
	for _, name := range names {
		doc, candidate, err := m.profileBackend(ctx, name)
		if err != nil {
			return err
		}
		candidateAddress := net.JoinHostPort(doc.Effective.Listen.Host, strconv.Itoa(doc.Effective.Listen.Port))
		if backend == "" {
			backend = candidate.Name()
			address = candidateAddress
			singleActive = candidate.Capabilities().SingleActiveProfile
		} else if candidate.Name() != backend {
			return Errorf(ErrorInvalidInput, "autostart profiles must use one backend; %q uses %q, not %q", name, candidate.Name(), backend)
		}
		if len(names) > 1 && !singleActive && candidateAddress != address {
			return Errorf(ErrorInvalidInput, "router autostart profiles must share one listen address; %q uses %s, not %s", name, candidateAddress, address)
		}
	}
	if singleActive && len(names) > 1 {
		return Errorf(ErrorInvalidInput, "backend %q is single-active; configure only one autostart profile", backend)
	}
	return nil
}

// AutostartProfiles starts the configured set in lexical order after recovery.
// Each profile gets a small, fixed retry budget; one failure never stops the
// daemon or hides another profile's outcome.
func (m *Manager) AutostartProfiles(ctx context.Context, names []string) error {
	return m.autostartProfiles(ctx, names, autostartAttempts, autostartBackoff)
}

func (m *Manager) autostartProfiles(ctx context.Context, names []string, attempts int, backoff time.Duration) error {
	names = slices.Clone(names)
	sort.Strings(names)
	for _, name := range names {
		if err := m.autostartProfile(ctx, name, attempts, backoff); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) autostartProfile(ctx context.Context, name string, attempts int, backoff time.Duration) error {
	started := time.Now()
	status, err := m.RuntimeStatus(ctx, name)
	if err != nil {
		return err
	}
	if status.State == coreruntime.Running {
		m.recordAutostart(ctx, name, "already running after reconciliation", started, nil)
		return nil
	}
	attempt, startErr := 0, error(nil)
	for attempt < attempts {
		attempt++
		_, startErr = m.StartRuntime(ctx, name, false)
		if startErr == nil {
			break
		}
		if attempt < attempts {
			select {
			case <-time.After(time.Duration(attempt) * backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	detail := fmt.Sprintf("started after %d attempt(s)", attempt)
	if startErr != nil {
		detail = fmt.Sprintf("failed after %d attempt(s): %v", attempt, startErr)
	}
	m.recordAutostart(ctx, name, detail, started, startErr)
	return nil
}

func (m *Manager) recordAutostart(ctx context.Context, profile, detail string, started time.Time, err error) {
	m.audit.Record(ctx, AuditEvent{Protocol: "core", Action: "runtime.autostart", Success: err == nil,
		ErrorKind: Kind(err), Duration: time.Since(started), Profile: profile, Detail: detail})
}
