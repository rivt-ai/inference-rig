package control

import (
	"bytes"
	"context"
	"os"
	"slices"

	"inferencerig/backends"
	coreruntime "inferencerig/core/runtime"
)

// materializationChanged reports whether any generated file differs from what
// is on disk. It is read before the write replaces the files, so a profile
// write that renders byte-identical output costs no runtime reload.
func materializationChanged(materialization backends.Materialization) bool {
	for _, generated := range materialization.Files {
		existing, err := os.ReadFile(generated.Path)
		if err != nil || !bytes.Equal(existing, generated.Content) {
			return true
		}
	}
	return false
}

// reloadRouter replaces a running router process after its generated preset
// file changed. A router engine reads that file once at startup, so a profile
// created, edited or deleted while the router runs stays invisible to it until
// the process is replaced. Every profile the slot held is re-activated in the
// replacement, and the slot keeps holding them. A single-profile backend or an
// idle slot needs nothing.
func (m *Manager) reloadRouter(ctx context.Context, backend backends.Backend) error {
	if backend.Capabilities().SingleActiveProfile {
		return nil
	}
	// Only a backend rendering one shared file for all profiles has startup
	// state a profile write can invalidate; per-profile files reach the router
	// through the start that uses them.
	if _, batch := backend.(batchMaterializer); !batch {
		return nil
	}
	m.mu.Lock()
	if m.slot == nil || m.slot.backend != backend.Name() {
		m.mu.Unlock()
		return nil
	}
	if !m.slot.settled() {
		defer m.mu.Unlock()
		return m.slot.conflict()
	}
	held := slices.Clone(m.slot.profiles)
	process := m.slot.process
	op := m.reserveLocked(held[0], m.slot.backend, coreruntime.Stopping)
	m.mu.Unlock()

	spec, err := m.routerSpec(ctx, backend, held[0])
	if err != nil {
		return m.fail(ctx, op, err, true)
	}
	m.transition(ctx, op, coreruntime.Stopping, nil)
	if process != nil {
		if _, err := process.Stop(ctx); err != nil {
			return m.fail(ctx, op, mapRuntimeError(err), true)
		}
	}
	m.transition(ctx, op, coreruntime.Starting, nil)
	replacement := m.factory(spec)
	result, err := replacement.Start(ctx)
	if err != nil {
		return m.fail(ctx, op, mapRuntimeError(err), false)
	}
	m.transition(ctx, op, coreruntime.Activating, nil)
	for _, name := range held {
		if p, err := m.GetProfile(ctx, name); err == nil {
			activate(ctx, backend, p.Effective, &result)
		}
	}
	m.commitStart(ctx, op, replacement)
	return nil
}

// routerSpec rebuilds the launch spec of the running router from one profile it
// holds. The router is bound to the address of whichever held profile started
// it, so any held profile's spec names the same process.
func (m *Manager) routerSpec(ctx context.Context, backend backends.Backend, profile string) (coreruntime.LaunchSpec, error) {
	doc, err := m.GetProfile(ctx, profile)
	if err != nil {
		return coreruntime.LaunchSpec{}, err
	}
	materialization, err := m.materialize(ctx, backend, doc.Effective)
	if err != nil {
		return coreruntime.LaunchSpec{}, CoreError(ErrorInvalidInput, err.Error(), err)
	}
	spec, err := backend.LaunchSpec(doc.Effective, materialization)
	if err != nil {
		return coreruntime.LaunchSpec{}, CoreError(ErrorInvalidInput, err.Error(), err)
	}
	return spec, nil
}
