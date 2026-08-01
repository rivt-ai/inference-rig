package control

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"inferencerig/backends"
	"inferencerig/core/profiles"
	coreruntime "inferencerig/core/runtime"
)

type recoveryCandidate struct {
	profile string
	backend backends.Backend
	spec    coreruntime.LaunchSpec
}

// RecoverRuntimes reconciles persisted supervisor PID files into the single
// runtime slot before the control server starts accepting requests.
func (m *Manager) RecoverRuntimes(ctx context.Context) error {
	docs, err := m.profiles.ListDocuments(ctx)
	if err != nil {
		return mapProfileError(err)
	}
	groups := map[string][]recoveryCandidate{}
	var order []string
	for _, doc := range docs {
		candidate, err := m.recoveryCandidate(ctx, doc)
		if err != nil {
			return err
		}
		key := filepath.Join(candidate.spec.PIDDir, candidate.spec.Name+".pid")
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], candidate)
	}
	for _, key := range order {
		settled, err := m.recoverGroup(ctx, groups[key])
		if err != nil || settled {
			return err
		}
	}
	return nil
}

func (m *Manager) recoveryCandidate(ctx context.Context, doc profiles.ProfileDocument) (recoveryCandidate, error) {
	backend, err := m.Backend(doc.Effective.Backend)
	if err != nil {
		return recoveryCandidate{}, err
	}
	materialization, err := m.materialize(ctx, backend, doc.Effective)
	if err != nil {
		return recoveryCandidate{}, CoreError(ErrorInvalidInput, err.Error(), err)
	}
	spec, err := backend.LaunchSpec(doc.Effective, materialization)
	if err != nil {
		return recoveryCandidate{}, CoreError(ErrorInvalidInput, err.Error(), err)
	}
	return recoveryCandidate{profile: doc.Name, backend: backend, spec: spec}, nil
}

func (m *Manager) recoverGroup(ctx context.Context, candidates []recoveryCandidate) (bool, error) {
	var failed recoveryAttempt
	for _, candidate := range candidates {
		attempt := m.beginRecovery(candidate)
		adopted, err := attempt.process.Recover(ctx)
		class := coreruntime.RecoveryClass(err)
		switch {
		case adopted:
			m.restoreRouterProfiles(attempt, candidates)
			m.finishRecovery(ctx, attempt, coreruntime.Running, coreruntime.RecoveryValidAdoptee, nil)
			return true, nil
		case err == nil:
			m.clearRecovery(attempt.op)
		case class != "":
			m.clearRecovery(attempt.op)
			failed = attempt
			failed.err = err
		default:
			m.clearRecovery(attempt.op)
			return false, mapRuntimeError(err)
		}
	}
	if failed.err == nil {
		return false, nil
	}
	class := coreruntime.RecoveryClass(failed.err)
	state := coreruntime.Orphaned
	if class == coreruntime.RecoveryStalePIDFile {
		state = coreruntime.Stopped
	}
	m.restoreRecovery(failed, state)
	m.finishRecovery(ctx, failed, state, class, failed.err)
	return state == coreruntime.Orphaned, nil
}

func (m *Manager) restoreRouterProfiles(attempt recoveryAttempt, candidates []recoveryCandidate) {
	if attempt.backend.Capabilities().SingleActiveProfile {
		return
	}
	address := net.JoinHostPort(attempt.spec.Host, strconv.Itoa(attempt.spec.Port))
	profiles := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.backend.Name() == attempt.backend.Name() &&
			net.JoinHostPort(candidate.spec.Host, strconv.Itoa(candidate.spec.Port)) == address {
			profiles = append(profiles, candidate.profile)
		}
	}
	m.mu.Lock()
	if m.slot != nil && m.slot.op == attempt.op.id {
		m.slot.profiles = profiles
	}
	m.mu.Unlock()
}

type recoveryAttempt struct {
	recoveryCandidate
	process Runtime
	op      operation
	err     error
}

func (m *Manager) beginRecovery(candidate recoveryCandidate) recoveryAttempt {
	process := m.factory(candidate.spec)
	m.mu.Lock()
	m.slot = &runtimeSlot{
		backend: candidate.backend.Name(), process: process,
		address:  net.JoinHostPort(candidate.spec.Host, strconv.Itoa(candidate.spec.Port)),
		profiles: []string{candidate.profile},
	}
	op := m.reserveLocked(candidate.profile, candidate.backend.Name(), coreruntime.Reconciling)
	m.mu.Unlock()
	return recoveryAttempt{recoveryCandidate: candidate, process: process, op: op}
}

func (m *Manager) clearRecovery(op operation) {
	m.mu.Lock()
	if m.slot != nil && m.slot.op == op.id {
		m.slot = nil
	}
	m.mu.Unlock()
}

func (m *Manager) restoreRecovery(attempt recoveryAttempt, state coreruntime.State) {
	m.mu.Lock()
	m.slot = &runtimeSlot{
		backend: attempt.backend.Name(), process: attempt.process,
		address:  net.JoinHostPort(attempt.spec.Host, strconv.Itoa(attempt.spec.Port)),
		profiles: []string{attempt.profile}, state: state, op: attempt.op.id,
	}
	m.mu.Unlock()
}

func (m *Manager) finishRecovery(ctx context.Context, attempt recoveryAttempt, state coreruntime.State, class coreruntime.RecoveryClassification, err error) {
	m.transition(ctx, attempt.op, coreruntime.Reconciling, nil)
	m.mu.Lock()
	if m.slot != nil && m.slot.op == attempt.op.id {
		if state == coreruntime.Stopped {
			m.slot = nil
		} else {
			m.slot.state, m.slot.op = state, ""
		}
	}
	m.mu.Unlock()
	m.audit.Record(ctx, AuditEvent{
		Protocol: "core", Action: "runtime.recover", Success: class == coreruntime.RecoveryValidAdoptee,
		ErrorKind: Kind(mapRuntimeError(err)),
		Duration:  time.Since(attempt.op.start), OperationID: attempt.op.id,
		Profile: attempt.profile, Backend: attempt.backend.Name(), State: state, Recovery: class,
	})
}
