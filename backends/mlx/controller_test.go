package mlx

import (
	"context"
	"testing"

	coreruntime "inferencerig/core/runtime"
)

type fakeProcess struct {
	starts, stops int
	state         coreruntime.State
}

func (f *fakeProcess) Start(context.Context) (coreruntime.CommandResult, error) {
	f.starts++
	f.state = coreruntime.Running
	return coreruntime.CommandResult{Action: "start"}, nil
}
func (f *fakeProcess) Stop(context.Context) (coreruntime.CommandResult, error) {
	f.stops++
	f.state = coreruntime.Stopped
	return coreruntime.CommandResult{Action: "stop"}, nil
}
func (f *fakeProcess) Status(context.Context) (coreruntime.Status, error) {
	return coreruntime.Status{State: f.state}, nil
}
func (f *fakeProcess) Recover(context.Context) (bool, error) { return false, nil }

func TestControllerSwitchStopsCurrent(t *testing.T) {
	controller := NewController()
	var made []*fakeProcess
	controller.new = func(coreruntime.LaunchSpec) processLifecycle {
		process := &fakeProcess{}
		made = append(made, process)
		return process
	}
	ctx := context.Background()
	if _, err := controller.Start(ctx, "first", coreruntime.LaunchSpec{}); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Start(ctx, "second", coreruntime.LaunchSpec{}); err != nil {
		t.Fatal(err)
	}
	if len(made) != 2 || made[0].stops != 1 || controller.ActiveProfile() != "second" {
		t.Fatalf("processes = %#v, active = %q", made, controller.ActiveProfile())
	}
	status, err := controller.Status(ctx)
	if err != nil || status.State != coreruntime.Running {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
	if _, err := controller.Stop(ctx); err != nil || controller.ActiveProfile() != "" {
		t.Fatalf("stop err = %v, active = %q", err, controller.ActiveProfile())
	}
}
