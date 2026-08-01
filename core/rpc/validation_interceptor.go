package rpc

import (
	"context"
	"reflect"

	"connectrpc.com/connect"

	"inferencerig/core/control"
)

// ValidateRequestInterceptor rejects unary requests with a nil message before
// they reach a handler, mapping the rejection to the invalid-input error kind.
func ValidateRequestInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if req == nil || isNilRequest(req.Any()) {
				return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "request is required"))
			}
			return next(ctx, req)
		}
	}
}

func isNilRequest(req any) bool {
	if req == nil {
		return true
	}
	value := reflect.ValueOf(req)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
