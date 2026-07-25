package runtime

import (
	"errors"
	"fmt"
	"time"
)

type State string

const (
	Running  State = "running"
	Stopped  State = "stopped"
	Starting State = "starting"
	Stopping State = "stopping"
	Failed   State = "failed"
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
