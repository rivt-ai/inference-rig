package setup

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"

	"gopkg.in/yaml.v3"
)

type testClient struct {
	controlv1connect.ControlServiceClient
	put *controlv1.PutProfileRequest
}

func (c *testClient) ListBackends(context.Context, *controlv1.ListBackendsRequest) (*controlv1.ListBackendsResponse, error) {
	return &controlv1.ListBackendsResponse{Backends: []*controlv1.BackendInfo{{
		Name: "test", Capabilities: &controlv1.BackendCapabilities{MultiFileArtifacts: true},
	}}}, nil
}

func (c *testClient) PutProfile(_ context.Context, request *controlv1.PutProfileRequest) (*controlv1.PutProfileResponse, error) {
	c.put = request
	return &controlv1.PutProfileResponse{Profile: &controlv1.Profile{Name: request.GetName(), Backend: "test"}}, nil
}

func TestWizardDiscoversCapabilitiesAndCreatesThroughRPC(t *testing.T) {
	client := &testClient{}
	wizard := NewWizard(client)
	backends, err := wizard.Backends(context.Background())
	if err != nil || !backends[0].GetCapabilities().GetMultiFileArtifacts() {
		t.Fatalf("backends = %#v, err = %v", backends, err)
	}
	profile, err := wizard.Create(context.Background(), Request{
		Name: "demo", Backend: "test", ModelSource: "repo", ModelReference: "model",
		Host: "127.0.0.1", Port: 8080,
	})
	if err != nil || profile.GetName() != "demo" || !strings.Contains(client.put.GetProfileYaml(), "backend: test") {
		t.Fatalf("profile = %#v, request = %#v, err = %v", profile, client.put, err)
	}
	var parsed profiles.Profile
	if err := yaml.Unmarshal([]byte(client.put.GetProfileYaml()), &parsed); err != nil || parsed.Model.Source != "repo" {
		t.Fatalf("profile YAML = %q, parsed = %#v, err = %v", client.put.GetProfileYaml(), parsed, err)
	}
}

func TestWizardInteractiveUsesCapabilities(t *testing.T) {
	client := &testClient{}
	var output bytes.Buffer
	profile, err := NewWizard(client).RunInteractive(
		context.Background(), strings.NewReader("\ndemo\nowner/repo\n\n9000\n"), &output,
	)
	if err != nil || profile.GetName() != "demo" {
		t.Fatalf("profile = %#v, output = %q, err = %v", profile, output.String(), err)
	}
	if strings.Contains(output.String(), "model reference") {
		t.Fatalf("multi-file backend prompted for a single-file reference: %q", output.String())
	}
}
