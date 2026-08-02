package lifecycle

import (
	"errors"

	"github.com/spf13/cobra"

	"inferencerig/platform/process"
)

// startDetached is a package var so tests can drive the startup-failure paths.
// StartDetached re-execs os.Executable(), which under `go test` is the test
// binary rather than the daemon, so the real call cannot be exercised here.
var startDetached = process.StartDetached

// errStartupFailed is what a command returns once it has already printed the
// daemon's own failure. Cobra and main both render a returned error, so the
// detail is written once, deliberately, and the returned value stays short
// enough that repeating it costs nothing.
var errStartupFailed = errors.New("control daemon failed to start")

// ReportStartupFailure prints a *process.StartupError as the daemon's own
// output — its error, its log path, and where to go next — and reduces it to a
// one-line error. Other errors pass through untouched.
func ReportStartupFailure(command *cobra.Command, err error) error {
	var failure *process.StartupError
	if !errors.As(err, &failure) {
		return err
	}
	_, _ = command.ErrOrStderr().Write([]byte(failure.Error() + "\n"))
	return errStartupFailed
}
