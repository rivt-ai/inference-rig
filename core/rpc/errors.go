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
var errorKindCodes = []struct {
	kind control.ErrorKind
	code connect.Code
}{
	{control.ErrorInvalidInput, connect.CodeInvalidArgument},
	{control.ErrorPermission, connect.CodePermissionDenied},
	{control.ErrorNotFound, connect.CodeNotFound},
	{control.ErrorConflict, connect.CodeFailedPrecondition},
	{control.ErrorTimeout, connect.CodeDeadlineExceeded},
	{control.ErrorRuntime, connect.CodeInternal},
}

// rpcError converts a control-plane error into a connect error, preserving the
// error kind both as a wire code and as a response header.
func rpcError(err error) error {
	if err == nil {
		return nil
	}
	kind := control.Kind(err)
	code := connect.CodeUnknown
	for _, m := range errorKindCodes {
		if m.kind == kind {
			code = m.code
			break
		}
	}
	connectErr := connect.NewError(code, err)
	if kind != "" {
		connectErr.Meta().Set(errorKindHeader, string(kind))
	}
	return connectErr
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
	for _, m := range errorKindCodes {
		if m.code == connectErr.Code() {
			return m.kind
		}
	}
	return control.ErrorRuntime
}
