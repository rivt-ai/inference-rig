// Package setup provides capability-aware canonical profile setup.
package setup

import (
	"context"
	"fmt"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

// Request describes the shared part of a profile setup.
type Request struct {
	Name, Backend, ModelSource, ModelReference, Host string
	Port                                             int
}

// Wizard discovers capabilities and writes profiles through canonical RPC.
type Wizard struct {
	client controlv1connect.ControlServiceClient
}

// NewWizard creates a setup wizard.
func NewWizard(client controlv1connect.ControlServiceClient) *Wizard {
	if client == nil {
		panic("setup: control client is required")
	}
	return &Wizard{client: client}
}

// Backends returns the available capability descriptors.
func (w *Wizard) Backends(ctx context.Context) ([]*controlv1.BackendInfo, error) {
	response, err := w.client.ListBackends(ctx, &controlv1.ListBackendsRequest{})
	if err != nil {
		return nil, err
	}
	return response.GetBackends(), nil
}

// Create validates backend availability and creates one canonical YAML profile.
func (w *Wizard) Create(ctx context.Context, request Request) (*controlv1.Profile, error) {
	backends, err := w.Backends(ctx)
	if err != nil {
		return nil, err
	}
	found := false
	for _, backend := range backends {
		found = found || backend.GetName() == request.Backend
	}
	if !found {
		return nil, fmt.Errorf("setup: backend %q is not available", request.Backend)
	}
	profileYAML := fmt.Sprintf(
		"version: 1\nname: %s\nbackend: %s\nmodel:\n  source: %s\n  reference: %s\nlisten:\n  host: %s\n  port: %d\n",
		request.Name, request.Backend, request.ModelSource, request.ModelReference, request.Host, request.Port,
	)
	response, err := w.client.PutProfile(ctx, &controlv1.PutProfileRequest{
		Name: request.Name, ProfileYaml: profileYAML, CreateOnly: true,
	})
	if err != nil {
		return nil, err
	}
	return response.GetProfile(), nil
}
