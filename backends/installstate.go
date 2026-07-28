package backends

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"inferencerig/platform/filedoc"
)

// stateFileName is the managed-install state document every backend keeps at
// the root of its engine directory.
const stateFileName = "state.json"

// installStateMode keeps the state document owner-readable only; it records
// local filesystem paths for a managed install.
const installStateMode = 0o600

// ReadInstallState decodes root/state.json into T. A backend defines its own
// state shape; only the location, encoding and missing-file handling are
// shared. A missing file yields the zero value and no error, so a first
// install needs no special case.
func ReadInstallState[T any](root string) (T, error) {
	var state T
	data, err := os.ReadFile(filepath.Join(root, stateFileName))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("read install state: %w", err)
	}
	return state, nil
}

// WriteInstallState atomically replaces root/state.json with state.
func WriteInstallState[T any](root string, state T) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	_, err = filedoc.WriteFile(
		filepath.Join(root, stateFileName),
		string(data)+"\n",
		filedoc.WriteOptions{Perm: installStateMode},
	)
	return err
}
