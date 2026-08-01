package modelcatalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"inferencerig/platform/filedoc"
)

func writeModel(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Recording at download time is what makes a later integrity check mean
// anything; without it there is nothing to compare against.
func TestDigestRoundTripDetectsCorruption(t *testing.T) {
	path := writeModel(t, "model bytes")
	const digest = "0d3b1b0a1a0e0b0c" // value is opaque here; only the compare matters

	if err := RecordDigest(path, digest); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDigest(path)
	if err != nil || got != digest {
		t.Fatalf("ReadDigest = %q, %v, want %q", got, err, digest)
	}

	// The recorded digest is not the file's real hash, so verification fails —
	// which is exactly the corruption signal.
	matched, err := VerifyDigest(path)
	if err != nil || matched {
		t.Errorf("VerifyDigest = %v, %v, want a mismatch", matched, err)
	}
}

func TestVerifyDigestAcceptsAnIntactFile(t *testing.T) {
	path := writeModel(t, "model bytes")
	sum, err := filedoc.SHA256File(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordDigest(path, sum); err != nil {
		t.Fatal(err)
	}

	matched, err := VerifyDigest(path)
	if err != nil || !matched {
		t.Errorf("VerifyDigest = %v, %v, want a match", matched, err)
	}
}

// Models predating digest recording have nothing to compare against. That is
// distinct from failing verification and callers must be able to tell.
func TestVerifyDigestReportsNoRecordedDigest(t *testing.T) {
	path := writeModel(t, "model bytes")

	if _, err := VerifyDigest(path); !errors.Is(err, ErrNoDigest) {
		t.Errorf("err = %v, want ErrNoDigest", err)
	}
	if err := RecordDigest(path, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDigest(path); !errors.Is(err, ErrNoDigest) {
		t.Errorf("recording an empty digest created a sidecar: %v", err)
	}
}

// The sidecar must never be listed as a model in its own right.
func TestDigestSidecarIsNotAModelExtension(t *testing.T) {
	if digestSuffix == ".gguf" || digestSuffix == "" {
		t.Fatalf("digestSuffix = %q collides with a model format", digestSuffix)
	}
}
