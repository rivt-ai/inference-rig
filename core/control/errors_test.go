package control

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestCoreErrorAndErrorf(t *testing.T) {
	cause := errors.New("boom")
	got := CoreError(ErrorNotFound, "missing", cause)
	if got.Kind != ErrorNotFound || got.Message != "missing" || !errors.Is(got, cause) {
		t.Fatalf("CoreError = %#v", got)
	}
	formatted := Errorf(ErrorConflict, "conflict on %q", "name")
	if formatted.Kind != ErrorConflict || formatted.Message != `conflict on "name"` || formatted.Err != nil {
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
		{"plain error", errors.New("plain"), ErrorInternal},
		{"typed error", CoreError(ErrorInvalidInput, "bad", nil), ErrorInvalidInput},
		{"wrapped typed error", fmt.Errorf("ctx: %w", CoreError(ErrorTimeout, "slow", nil)), ErrorTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Kind(tc.err); got != tc.want {
				t.Fatalf("Kind = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMapSentinel(t *testing.T) {
	table := []SentinelKind{
		{Target: os.ErrNotExist, Kind: ErrorNotFound},
		{Target: os.ErrPermission, Kind: ErrorPermission},
	}

	if err := MapSentinel(nil, table); err != nil {
		t.Fatalf("MapSentinel(nil) = %v, want nil", err)
	}

	wrapped := fmt.Errorf("open profile: %w", os.ErrNotExist)
	mapped := MapSentinel(wrapped, table)
	var coreErr *Error
	if !errors.As(mapped, &coreErr) {
		t.Fatalf("MapSentinel did not return *Error, got %T", mapped)
	}
	if coreErr.Kind != ErrorNotFound {
		t.Fatalf("mapped kind = %q, want %q", coreErr.Kind, ErrorNotFound)
	}
	if coreErr.Message != wrapped.Error() {
		t.Fatalf("mapped message = %q, want %q", coreErr.Message, wrapped.Error())
	}
	if !errors.Is(mapped, os.ErrNotExist) {
		t.Fatalf("mapped error lost its sentinel chain")
	}

	other := errors.New("unrelated")
	if got := MapSentinel(other, table); got != other {
		t.Fatalf("MapSentinel(non-matching) = %v, want the original error", got)
	}
}
