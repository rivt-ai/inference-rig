// Package setup provides capability-aware canonical profile setup.
package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"

	"gopkg.in/yaml.v3"
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
	if backendByName(backends, request.Backend) == nil {
		return nil, fmt.Errorf("setup: backend %q is not available", request.Backend)
	}
	data, err := yaml.Marshal(profiles.Profile{
		Version: "1", Name: request.Name, Backend: request.Backend,
		Model:  profiles.ModelSpec{Source: request.ModelSource, Reference: request.ModelReference},
		Listen: profiles.ListenSpec{Host: request.Host, Port: request.Port},
	})
	if err != nil {
		return nil, err
	}
	response, err := w.client.PutProfile(ctx, &controlv1.PutProfileRequest{
		Name: request.Name, ProfileYaml: string(data), CreateOnly: true,
	})
	if err != nil {
		return nil, err
	}
	return response.GetProfile(), nil
}

// RunInteractive prompts for the minimum capability-aware profile fields.
func (w *Wizard) RunInteractive(ctx context.Context, input io.Reader, output io.Writer) (*controlv1.Profile, error) {
	backends, err := w.Backends(ctx)
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReader(input)
	selected, err := selectBackend(reader, output, backends)
	if err != nil {
		return nil, err
	}
	p := &prompter{reader: reader, output: output}
	name := p.ask("profile name", "default")
	source := p.ask("model source", "")
	reference := ""
	if selected.GetCapabilities().GetSingleFileArtifacts() {
		reference = p.ask("model reference", "")
	}
	host := p.ask("listen host", "127.0.0.1")
	portText := p.ask("listen port", "8080")
	if p.err != nil {
		return nil, p.err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, fmt.Errorf("setup: invalid port %q", portText)
	}
	return w.Create(ctx, Request{
		Name: name, Backend: selected.GetName(), ModelSource: source,
		ModelReference: reference, Host: host, Port: port,
	})
}

// prompter accumulates the first prompt error the way bufio.Scanner does, so a
// questionnaire reads as a list of questions rather than as error handling.
// Once err is set, later asks return immediately without prompting.
type prompter struct {
	reader *bufio.Reader
	output io.Writer
	err    error
}

func (p *prompter) ask(label, fallback string) string {
	if p.err != nil {
		return ""
	}
	value, err := prompt(p.reader, p.output, label, fallback)
	p.err = err
	return value
}

func selectBackend(reader *bufio.Reader, output io.Writer, backends []*controlv1.BackendInfo) (*controlv1.BackendInfo, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("setup: no backends are available")
	}
	for _, backend := range backends {
		_, _ = fmt.Fprintln(output, backend.GetName())
	}
	name, err := prompt(reader, output, "backend", backends[0].GetName())
	if err != nil {
		return nil, err
	}
	selected := backendByName(backends, name)
	if selected == nil {
		return nil, fmt.Errorf("setup: backend %q is not available", name)
	}
	return selected, nil
}

func prompt(reader *bufio.Reader, output io.Writer, label, fallback string) (string, error) {
	if fallback == "" {
		_, _ = fmt.Fprintf(output, "%s: ", label)
	} else {
		_, _ = fmt.Fprintf(output, "%s [%s]: ", label, fallback)
	}
	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if value == "" {
		return "", fmt.Errorf("setup: %s is required", label)
	}
	return value, nil
}

func backendByName(backends []*controlv1.BackendInfo, name string) *controlv1.BackendInfo {
	for _, backend := range backends {
		if backend.GetName() == name {
			return backend
		}
	}
	return nil
}
