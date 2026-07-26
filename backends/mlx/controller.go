package mlx

import (
	"context"
	"sync"

	coreruntime "inferencerig/core/runtime"
)

type processLifecycle interface {
	Start(context.Context) (coreruntime.CommandResult, error)
	Stop(context.Context) (coreruntime.CommandResult, error)
	Status(context.Context) (coreruntime.Status, error)
	Recover(context.Context) (bool, error)
}

// Controller enforces MLX's one-active-profile runtime policy while delegating
// every process operation to the shared supervisor.
type Controller struct {
	mu      sync.Mutex
	active  string
	process processLifecycle
	new     func(coreruntime.LaunchSpec) processLifecycle
}

// NewController creates an idle MLX runtime controller.
func NewController() *Controller {
	return &Controller{new: func(spec coreruntime.LaunchSpec) processLifecycle {
		return coreruntime.NewSupervisor(spec)
	}}
}

// Start starts profile, stopping a different active profile first.
func (c *Controller) Start(ctx context.Context, profile string, spec coreruntime.LaunchSpec) (coreruntime.CommandResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.process != nil {
		status, err := c.process.Status(ctx)
		if err != nil {
			return coreruntime.CommandResult{}, err
		}
		if c.active == profile && status.State == coreruntime.Running {
			return coreruntime.CommandResult{Action: "start"}, nil
		}
		if _, err := c.process.Stop(ctx); err != nil {
			return coreruntime.CommandResult{}, err
		}
	}
	next := c.new(spec)
	result, err := next.Start(ctx)
	if err != nil {
		c.active, c.process = "", nil
		return result, err
	}
	c.active, c.process = profile, next
	return result, nil
}

// Stop stops the active profile. It is idempotent.
func (c *Controller) Stop(ctx context.Context) (coreruntime.CommandResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.process == nil {
		return coreruntime.CommandResult{Action: "stop"}, nil
	}
	result, err := c.process.Stop(ctx)
	if err == nil {
		c.active, c.process = "", nil
	}
	return result, err
}

// Status reports the active profile's shared-supervisor state.
func (c *Controller) Status(ctx context.Context) (coreruntime.Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.process == nil {
		return coreruntime.Status{State: coreruntime.Stopped}, nil
	}
	return c.process.Status(ctx)
}

// Recover adopts profile from its shared-supervisor PID file.
func (c *Controller) Recover(ctx context.Context, profile string, spec coreruntime.LaunchSpec) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	process := c.new(spec)
	ok, err := process.Recover(ctx)
	if err == nil && ok {
		c.active, c.process = profile, process
	}
	return ok, err
}

// ActiveProfile returns the currently managed profile name.
func (c *Controller) ActiveProfile() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}
