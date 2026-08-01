package modelcatalog

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"inferencerig/platform/filedoc"
)

// digestSuffix names the sidecar recording a model's verified digest.
//
// A sidecar rather than a central index because models are moved, copied and
// deleted with ordinary file tools; an index would silently describe files that
// are no longer there. No FormatPolicy accepts this extension, so a sidecar is
// never itself listed as a model.
const digestSuffix = ".sha256"

// ErrNoDigest reports that nothing was recorded for a model. Models that
// predate digest recording have nothing to compare against, which is not the
// same as failing verification.
var ErrNoDigest = errors.New("no digest recorded for this model")

// RecordDigest stores the verified digest of the model at path.
//
// The download path already hashes every artifact to check it against the
// catalog and then discards the result. Keeping it is what lets a later
// integrity check mean anything.
func RecordDigest(path, sha256Hex string) error {
	if sha256Hex == "" {
		return nil
	}
	return filedoc.AtomicCreate(path+digestSuffix, []byte(sha256Hex+"\n"), 0o600)
}

// ReadDigest returns the digest recorded for the model at path.
func ReadDigest(path string) (string, error) {
	data, err := os.ReadFile(path + digestSuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoDigest
	}
	if err != nil {
		return "", err
	}
	digest := strings.TrimSpace(string(data))
	if digest == "" {
		return "", ErrNoDigest
	}
	return digest, nil
}

// VerifyDigest re-hashes the model at path against its recorded digest.
// It returns ErrNoDigest when there is nothing to compare against.
func VerifyDigest(path string) (bool, error) {
	want, err := ReadDigest(path)
	if err != nil {
		return false, err
	}
	got, err := filedoc.SHA256File(path)
	if err != nil {
		return false, err
	}
	return got == want, nil
}
