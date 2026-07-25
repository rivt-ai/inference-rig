// Package control holds the neutral error taxonomy, audit event types, and the
// in-memory event store shared by the control plane. Backend-specific
// controllers and the canonical RPC service (Phase 9) build on these primitives.
package control

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrorInvalidInput ErrorKind = "invalid_input"
	ErrorNotFound     ErrorKind = "not_found"
	ErrorConflict     ErrorKind = "conflict"
	ErrorRuntime      ErrorKind = "runtime_error"
	ErrorTimeout      ErrorKind = "timeout"
	ErrorPermission   ErrorKind = "permission"
	ErrorInternal     ErrorKind = "internal"
)

type Error struct {
	Kind    ErrorKind `json:"kind"`
	Message string    `json:"message"`
	Err     error     `json:"-"`
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

func CoreError(kind ErrorKind, message string, err error) *Error {
	return &Error{Kind: kind, Message: message, Err: err}
}

func Errorf(kind ErrorKind, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

func Kind(err error) ErrorKind {
	if err == nil {
		return ""
	}
	if coreErr, ok := errors.AsType[*Error](err); ok {
		return coreErr.Kind
	}
	return ErrorInternal
}

func Message(err error) string {
	if err == nil {
		return ""
	}
	if coreErr, ok := errors.AsType[*Error](err); ok {
		return coreErr.Message
	}
	return err.Error()
}

// SentinelKind pairs a sentinel error with the ErrorKind it maps to.
type SentinelKind struct {
	Target error
	Kind   ErrorKind
}

// MapSentinel returns a CoreError for the first table entry whose Target
// matches err (via errors.Is), preserving err.Error() as the message. If no
// entry matches, err is returned unchanged.
func MapSentinel(err error, table []SentinelKind) error {
	if err == nil {
		return nil
	}
	for _, e := range table {
		if errors.Is(err, e.Target) {
			return CoreError(e.Kind, err.Error(), err)
		}
	}
	return err
}

func MessageOr(fallback string, err error) string {
	if err == nil || err.Error() == "" {
		return fallback
	}
	return err.Error()
}
