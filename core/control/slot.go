package control

import (
	"context"
	"fmt"
	"slices"
	"time"

	"inferencerig/backends"
	coreruntime "inferencerig/core/runtime"
)

// runtimeSlot is the single runtime the manager owns. InferenceRig serves one
// backend at a time, so at most one slot exists: while it does, its backend is
// the active backend and a profile naming any other backend cannot start.
//
// An exclusive backend (Capabilities.SingleActiveProfile) holds exactly one
// profile here; a router backend (which implements backends.RuntimeActivator)
// holds every profile activated inside its one process.
//
// state is authoritative. It is never inferred from the profile set or from
// whether a process object exists, because both are true long before the engine
// is serving and stay true while it is being torn down.
type runtimeSlot struct {
	backend  string
	process  Runtime
	profiles []string
	state    coreruntime.State
	// op is the ID of the transition currently owning the slot, empty when the
	// slot is at rest. Only the owning operation may commit a result.
	op string
}

// settled reports whether the slot is in a state a lifecycle call may act on.
// Anything else — a transition in flight, a failed stop, a runtime ticket 05
// classified as orphaned — needs the state resolved first, so the caller gets a
// typed conflict naming the state rather than being queued behind it.
func (s *runtimeSlot) settled() bool { return s.state == coreruntime.Running }

func (s *runtimeSlot) holds(profile string) bool { return slices.Contains(s.profiles, profile) }

// operation identifies one lifecycle transition. The ID ties every transition
// record of a single start, stop or reset together, and later observability work
// reuses it to join control, gateway and engine logs.
type operation struct {
	id      string
	profile string
	backend string
	start   time.Time
}

// reserveLocked claims the slot for a new operation. The caller holds m.mu; the
// blocking work the operation describes must then run with the lock released.
func (m *Manager) reserveLocked(profile, backend string, state coreruntime.State) operation {
	m.ops++
	op := operation{id: fmt.Sprintf("op-%d", m.ops), profile: profile, backend: backend, start: time.Now()}
	m.slot.state, m.slot.op = state, op.id
	return op
}

// transition records one state change of the slot to the audit sink, which also
// feeds the event stream. Every transition carries the operation ID, profile,
// backend and typed result, so a caller watching events sees the whole lifecycle
// without polling status.
func (m *Manager) transition(ctx context.Context, op operation, state coreruntime.State, err error) {
	m.audit.Record(ctx, AuditEvent{
		Protocol: "core", Action: "runtime.transition", Success: err == nil,
		ErrorKind: Kind(err), Duration: time.Since(op.start),
		OperationID: op.id, Profile: op.profile, Backend: op.backend, State: state,
	})
}

// conflict is the typed loser's error for a slot that cannot take the call.
func (s *runtimeSlot) conflict() error {
	return Errorf(ErrorConflict, "backend %q runtime is %s", s.backend, s.state)
}

// startPlan is what reserveStart decided, executed outside the lock: stop the
// process it names (a replace), spawn a new one, or neither — a router backend
// activating a second profile inside the process it already runs.
type startPlan struct {
	op    operation
	stop  Runtime
	spawn bool
}

// reserveStart claims the slot for starting profile in backend and returns the
// work to do. Every rejection here is a typed conflict: no call waits for
// another, so concurrent lifecycle calls on one slot resolve deterministically
// in the order they reach the lock.
func (m *Manager) reserveStart(backend backends.Backend, profile string, replace bool) (startPlan, error) {
	name := backend.Name()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.slot == nil {
		m.slot = &runtimeSlot{backend: name, profiles: []string{profile}}
		return startPlan{op: m.reserveLocked(profile, name, coreruntime.Starting), spawn: true}, nil
	}
	if !m.slot.settled() {
		return startPlan{}, m.slot.conflict()
	}
	if m.slot.backend != name {
		return startPlan{}, Errorf(ErrorConflict,
			"backend %q is active; reset the runtime before starting %q profile %q", m.slot.backend, name, profile)
	}
	if !backend.Capabilities().SingleActiveProfile {
		// Router backend: the running process takes another profile, so there is
		// nothing to stop and nothing to spawn.
		if !m.slot.holds(profile) {
			m.slot.profiles = append(m.slot.profiles, profile)
		}
		return startPlan{op: m.reserveLocked(profile, name, coreruntime.Activating)}, nil
	}
	if !replace {
		if m.slot.holds(profile) {
			return startPlan{}, Errorf(ErrorConflict, "profile %q is already running", profile)
		}
		return startPlan{}, Errorf(ErrorConflict,
			"profile %q is running on backend %q; retry with replace to stop it first", m.slot.profiles[0], name)
	}
	// Exclusive backend: the slot's one profile becomes this one, and its process
	// is stopped before the replacement spawns. A failure below drops the slot,
	// so a half-finished replace never leaves the backend claimed.
	plan := startPlan{op: m.reserveLocked(profile, name, coreruntime.Stopping), stop: m.slot.process, spawn: true}
	m.slot.profiles = []string{profile}
	return plan, nil
}

// stopPlan is what reserveStop decided. A nil process with a claimed operation
// means the slot holds other profiles too, so this one is only dropped from the
// set and the shared process keeps running.
type stopPlan struct {
	op      operation
	process Runtime
}

func (m *Manager) reserveStop(backend, profile string) (stopPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.slot == nil || m.slot.backend != backend || !m.slot.holds(profile) {
		return stopPlan{}, nil
	}
	if !m.slot.settled() {
		return stopPlan{}, m.slot.conflict()
	}
	op := m.reserveLocked(profile, backend, coreruntime.Stopping)
	if len(m.slot.profiles) > 1 {
		return stopPlan{op: op}, nil
	}
	return stopPlan{op: op, process: m.slot.process}, nil
}

// commitStart installs the operation's result. process is nil when no new
// process was spawned, which leaves the running one in place.
func (m *Manager) commitStart(ctx context.Context, op operation, process Runtime) {
	m.mu.Lock()
	if m.slot != nil && m.slot.op == op.id {
		if process != nil {
			m.slot.process = process
		}
		m.slot.state, m.slot.op = coreruntime.Running, ""
	}
	m.mu.Unlock()
	m.transition(ctx, op, coreruntime.Running, nil)
}

// commitStop drops the operation's profile, and the slot with it once the last
// profile is gone — which is what clears the active backend.
func (m *Manager) commitStop(ctx context.Context, op operation) {
	m.mu.Lock()
	if m.slot != nil && m.slot.op == op.id {
		m.slot.profiles = slices.DeleteFunc(m.slot.profiles, func(p string) bool { return p == op.profile })
		if len(m.slot.profiles) == 0 {
			m.slot = nil
		} else {
			m.slot.state, m.slot.op = coreruntime.Running, ""
		}
	}
	m.mu.Unlock()
	m.transition(ctx, op, coreruntime.Stopped, nil)
}

// fail ends the operation with err. keep leaves the slot behind in the failed
// state, which is right when a process may have survived a failed stop: the
// backend stays claimed and every later call conflicts until an explicit reset.
// A failed start drops the slot instead, because nothing is running and holding
// the active backend would block a backend that could have started.
func (m *Manager) fail(ctx context.Context, op operation, err error, keep bool) error {
	m.mu.Lock()
	if m.slot != nil && m.slot.op == op.id {
		if keep {
			m.slot.state, m.slot.op = coreruntime.Failed, ""
		} else {
			m.slot = nil
		}
	}
	m.mu.Unlock()
	m.transition(ctx, op, coreruntime.Failed, err)
	return err
}
