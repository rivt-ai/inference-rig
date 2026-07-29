package runtime

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorMessageAndUnwrap(t *testing.T) {
	cause := errors.New("boom")
	populated := NewError(ErrorRuntime, "failed", cause)
	if populated.Error() != "failed" {
		t.Fatalf("Error() = %q", populated.Error())
	}
	if !errors.Is(populated, cause) {
		t.Fatalf("Unwrap did not expose cause")
	}

	var nilErr *Error
	if nilErr.Error() != "" {
		t.Fatalf("nil Error() = %q, want empty", nilErr.Error())
	}
	if nilErr.Unwrap() != nil {
		t.Fatalf("nil Unwrap() = %v, want nil", nilErr.Unwrap())
	}
}

func TestNewErrorAndErrorf(t *testing.T) {
	got := NewError(ErrorInvalidInput, "bad value", nil)
	if got.Kind != ErrorInvalidInput || got.Message != "bad value" || got.Err != nil {
		t.Fatalf("NewError = %#v", got)
	}
	formatted := Errorf(ErrorTimeout, "timed out after %ds", 5)
	if formatted.Kind != ErrorTimeout || formatted.Message != "timed out after 5s" || formatted.Err != nil {
		t.Fatalf("Errorf = %#v", formatted)
	}
}

func TestKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"nil", nil, ""},
		{"plain error", errors.New("plain"), ""},
		{"typed error", NewError(ErrorRuntime, "runtime failure", nil), ErrorRuntime},
		{"wrapped typed error", fmt.Errorf("context: %w", NewError(ErrorInvalidInput, "bad", nil)), ErrorInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Kind(tc.err); got != tc.want {
				t.Fatalf("Kind = %q, want %q", got, tc.want)
			}
		})
	}
}
