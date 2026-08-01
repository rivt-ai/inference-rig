package runtime

import (
	"errors"
	"fmt"
	"time"
)

// RecoveryClassification is the stable vocabulary used when reconciling a PID
// file with a process that survived its control daemon.
type RecoveryClassification string

const (
	RecoveryStalePIDFile         RecoveryClassification = "stale_pid_file"
	RecoveryMismatchedExecutable RecoveryClassification = "mismatched_executable"
	RecoveryOccupiedPort         RecoveryClassification = "occupied_port"
	RecoveryUnhealthySurvivor    RecoveryClassification = "unhealthy_survivor"
	RecoveryValidAdoptee         RecoveryClassification = "valid_adoptee"
)

// RecoveryError reports why a recorded process could not be safely adopted.
type RecoveryError struct {
	Classification RecoveryClassification
	Message        string
	Err            error
}

func (e *RecoveryError) Error() string { return e.Message }
func (e *RecoveryError) Unwrap() error { return e.Err }

// RecoveryClass returns the reconciliation classification carried by err.
func RecoveryClass(err error) RecoveryClassification {
	if recoveryErr, ok := errors.AsType[*RecoveryError](err); ok {
		return recoveryErr.Classification
	}
	return ""
}

type State string

// A supervised process reports Running, Stopped, Starting, Stopping or Failed.
// The remaining states describe a control-plane runtime slot, which knows things
// one process cannot: that it is being matched against what is already on the
// host, that its engine is loading the profile it was started for, or that a
// live process was found which no longer belongs to anyone.
const (
	Running     State = "running"
	Stopped     State = "stopped"
	Starting    State = "starting"
	Stopping    State = "stopping"
	Failed      State = "failed"
	Reconciling State = "reconciling"
	Activating  State = "activating"
	Orphaned    State = "orphaned"
)

type Status struct {
	State     State           `json:"state"`
	Detail    string          `json:"detail,omitempty"`
	CheckedAt time.Time       `json:"checked_at"`
	Processes []ProcessStatus `json:"processes,omitempty"`
}

type ProcessStatus struct {
	Name      string `json:"name"`
	State     State  `json:"state"`
	PID       int    `json:"pid,omitempty"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	Ready     bool   `json:"ready"`
	LastError string `json:"last_error,omitempty"`
}

type CommandResult struct {
	Action     string `json:"action"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type ErrorKind string

const (
	ErrorInvalidInput ErrorKind = "invalid_input"
	ErrorRuntime      ErrorKind = "runtime_error"
	ErrorTimeout      ErrorKind = "timeout"
)

type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewError(kind ErrorKind, message string, err error) *Error {
	return &Error{Kind: kind, Message: message, Err: err}
}

func Errorf(kind ErrorKind, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

func Kind(err error) ErrorKind {
	if err == nil {
		return ""
	}
	if runtimeErr, ok := errors.AsType[*Error](err); ok {
		return runtimeErr.Kind
	}
	return ""
}
