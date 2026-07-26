package cli

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

type routeRecorder struct {
	mu   sync.Mutex
	path string
}

func (r *routeRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.path = request.URL.Path
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusNotImplemented, Status: "501 Not Implemented",
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(`{"code":"unimplemented"}`)),
	}, nil
}

func (r *routeRecorder) called() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.path
}

func TestEveryCommandCallsCanonicalRPC(t *testing.T) {
	profilePath := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(profilePath, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args      []string
		procedure string
	}{
		{[]string{"health"}, controlv1connect.ControlServiceHealthProcedure},
		{[]string{"info"}, controlv1connect.ControlServiceGetInfoProcedure},
		{[]string{"profile", "list"}, controlv1connect.ControlServiceListProfilesProcedure},
		{[]string{"profile", "get", "demo"}, controlv1connect.ControlServiceGetProfileProcedure},
		{[]string{"profile", "create", "demo", profilePath}, controlv1connect.ControlServicePutProfileProcedure},
		{[]string{"profile", "edit", "demo", profilePath}, controlv1connect.ControlServicePutProfileProcedure},
		{[]string{"profile", "delete", "demo"}, controlv1connect.ControlServiceDeleteProfileProcedure},
		{[]string{"profile", "cleanup", "demo"}, controlv1connect.ControlServiceCleanupProfileProcedure},
		{[]string{"profile", "autostart", "demo", "true"}, controlv1connect.ControlServiceSetProfileAutostartProcedure},
		{[]string{"model", "search", "test", "query"}, controlv1connect.ControlServiceListModelCatalogProcedure},
		{[]string{"model", "watch"}, controlv1connect.ControlServiceWatchModelCatalogProcedure},
		{[]string{"model", "list", "test"}, controlv1connect.ControlServiceListLocalModelsProcedure},
		{[]string{"model", "resolve", "demo"}, controlv1connect.ControlServiceResolveProfileModelProcedure},
		{[]string{"model", "download", "demo"}, controlv1connect.ControlServiceStartModelDownloadProcedure},
		{[]string{"model", "get", "dl"}, controlv1connect.ControlServiceGetModelDownloadProcedure},
		{[]string{"model", "cancel", "dl"}, controlv1connect.ControlServiceCancelModelDownloadProcedure},
		{[]string{"model", "apply", "demo", "dl"}, controlv1connect.ControlServiceApplyDownloadToProfileProcedure},
		{[]string{"model", "rm", "test", "/models/model"}, controlv1connect.ControlServiceDeleteLocalModelProcedure},
		{[]string{"backend", "list"}, controlv1connect.ControlServiceListBackendsProcedure},
		{[]string{"backend", "install", "test"}, controlv1connect.ControlServiceInstallBackendProcedure},
		{[]string{"backend", "params", "test"}, controlv1connect.ControlServiceGetBackendParamsProcedure},
		{[]string{"runtime", "status", "demo"}, controlv1connect.ControlServiceGetRuntimeStatusProcedure},
		{[]string{"runtime", "start", "demo"}, controlv1connect.ControlServiceStartRuntimeProcedure},
		{[]string{"runtime", "stop", "demo"}, controlv1connect.ControlServiceStopRuntimeProcedure},
		{[]string{"runtime", "restart", "demo"}, controlv1connect.ControlServiceRestartRuntimeProcedure},
		{[]string{"signals"}, controlv1connect.ControlServiceGetSignalsProcedure},
		{[]string{"events", "list"}, controlv1connect.ControlServiceListEventsProcedure},
		{[]string{"events", "watch"}, controlv1connect.ControlServiceWatchEventsProcedure},
		{[]string{"config", "startup", "control"}, controlv1connect.ControlServiceSetStartupServicesProcedure},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, "/"), func(t *testing.T) {
			recorder := &routeRecorder{}
			dial := func(string, time.Duration) (controlv1connect.ControlServiceClient, error) {
				return controlv1connect.NewControlServiceClient(&http.Client{Transport: recorder}, "http://control"), nil
			}
			root := &cobra.Command{Use: "test"}
			root.AddCommand(Commands(dial)...)
			root.SetArgs(test.args)
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			_ = root.ExecuteContext(context.Background())
			if got := recorder.called(); got != test.procedure {
				t.Fatalf("called %q, want %q", got, test.procedure)
			}
		})
	}
}
