package wardentest

import "testing"

func TestValues(t *testing.T) {
	if One() != 1 || Two() != 2 {
		t.Fatal("wrong values")
	}
}
