package lifecycle

import (
	"context"
	"errors"
	"testing"

	"inferencerig/config"
	controlv1 "inferencerig/core/rpc/gen/v1"
)

type probeControlClient struct {
	err   error
	calls int
}

func (s *probeControlClient) Health(context.Context, *controlv1.HealthRequest) (*controlv1.HealthResponse, error) {
	s.calls++
	return &controlv1.HealthResponse{}, s.err
}

// The PID file is removed on any failed start, so a healthy daemon can be
// serving with no PID file on disk. Starting a second one then fails to bind
// the socket the first still holds, which removes the PID file again and makes
// the failure repeat on every run. A daemon that answers must be left alone.
func TestEnsureControlDoesNotStartWhenDaemonAnswers(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir()) // no PID file exists here

	client := &probeControlClient{}
	started, err := ensureControl(context.Background(), client)
	if err != nil {
		t.Fatalf("ensureControl returned error: %v", err)
	}
	if started {
		t.Fatal("started a second daemon while one was already answering")
	}
	if client.calls == 0 {
		t.Fatal("ensureControl never probed the socket")
	}
}

// When nothing answers, the start path still runs. StartDetached fails here
// because the temporary home has no real binary to launch, which is enough to
// show the probe did not short-circuit it.
func TestEnsureControlStartsWhenNothingAnswers(t *testing.T) {
	t.Setenv(config.ProjectHomeEnv, t.TempDir())

	client := &probeControlClient{err: errors.New("connection refused")}
	if _, err := ensureControl(context.Background(), client); err == nil {
		t.Fatal("expected the start path to run and fail")
	}
	if client.calls == 0 {
		t.Fatal("ensureControl never probed the socket")
	}
}
