package rpc

import (
	"errors"

	"connectrpc.com/connect"

	"inferencerig/config"
	"inferencerig/core/control"
)

// errorKindHeader carries the control error kind alongside the connect wire
// code so clients can recover the original taxonomy.
const errorKindHeader = config.ProjectDisplayName + "-Error-Kind"

// errorKindCodes is the single source of truth pairing control error kinds
// with their connect wire codes, used in both directions by rpcError and
// ErrorKindFromRPC. An unlisted kind maps to CodeUnknown; an unlisted code
// maps back to ErrorRuntime.
var errorKindCodes = map[control.ErrorKind]connect.Code{
	control.ErrorInvalidInput: connect.CodeInvalidArgument,
	control.ErrorPermission:   connect.CodePermissionDenied,
	control.ErrorNotFound:     connect.CodeNotFound,
	control.ErrorConflict:     connect.CodeFailedPrecondition,
	control.ErrorTimeout:      connect.CodeDeadlineExceeded,
	control.ErrorRuntime:      connect.CodeInternal,
}

// rpcError converts a control-plane error into a connect error, preserving the
// error kind both as a wire code and as a response header.
func rpcError(err error) error {
	if err == nil {
		return nil
	}
	kind := control.Kind(err)
	code, ok := errorKindCodes[kind]
	if !ok {
		code = connect.CodeUnknown
	}
	connectErr := connect.NewError(code, err)
	if kind != "" {
		connectErr.Meta().Set(errorKindHeader, string(kind))
	}
	return connectErr
}

// requireField rejects an empty required request field, naming it in the
// error so the caller knows what was missing.
func requireField(value, field string) error {
	if value == "" {
		return rpcError(control.Errorf(control.ErrorInvalidInput, "%s is required", field))
	}
	return nil
}

// ErrorKindFromRPC recovers the control error kind from a connect error,
// preferring the explicit header and falling back to the wire code.
func ErrorKindFromRPC(err error) control.ErrorKind {
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return control.Kind(err)
	}
	if kind := connectErr.Meta().Get(errorKindHeader); kind != "" {
		return control.ErrorKind(kind)
	}
	// Codes are unique across the table, so iteration order does not matter.
	for kind, code := range errorKindCodes {
		if code == connectErr.Code() {
			return kind
		}
	}
	return control.ErrorRuntime
}
