package backends_test

import (
	"testing"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
)

// TestFakeSatisfiesContract runs the reusable contract suite against the fake
// backend, proving the suite is honored by a conforming implementation.
func TestFakeSatisfiesContract(t *testing.T) {
	backendtest.RunContractTests(t, func() backends.Backend {
		return backendtest.New("fake")
	})
}
