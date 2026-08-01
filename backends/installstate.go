package backends

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"inferencerig/platform/filedoc"
)

// stateFileName is the managed-install state document every backend keeps at
// the root of its engine directory.
const stateFileName = "state.json"

// installStateMode keeps the state document owner-readable only; it records
// local filesystem paths for a managed install.
const installStateMode = 0o600

// ErrNoPreviousInstall marks a rollback with no recorded install to return to.
var ErrNoPreviousInstall = errors.New("no previous managed install recorded")

// InstallRecord is the machine-readable record of one managed engine
// installation. It is the single neutral shape every backend writes and the
// release receipt and diagnostics read; engine specifics survive only as opaque
// strings in Source, Digest and Accelerator.
type InstallRecord struct {
	Backend string `json:"backend"`
	Version string `json:"version"`
	// Source is where the payload came from — a release asset URL, a package
	// index, a lock file. Free-form but stable per backend.
	Source string `json:"source"`
	// Digest is the verified content digest of the payload, prefixed with its
	// algorithm ("sha256:..."), when the backend has one.
	Digest string `json:"digest,omitempty"`
	// Platform is the GOOS/GOARCH the payload was installed for.
	Platform string `json:"platform"`
	// Accelerator is the compute backend the payload was selected for, when the
	// engine has that axis.
	Accelerator string    `json:"accelerator,omitempty"`
	Directory   string    `json:"directory"`
	Executable  string    `json:"executable"`
	InstalledAt time.Time `json:"installed_at"`
}

// InstallState is the document at <root>/state.json: the active installation
// and the one a rollback returns to. Retention keeps exactly these two.
type InstallState struct {
	Active   *InstallRecord `json:"active,omitempty"`
	Previous *InstallRecord `json:"previous,omitempty"`
}

// ReadInstallState decodes root/state.json. A missing file yields the zero
// value and no error, so a first install needs no special case.
func ReadInstallState(root string) (InstallState, error) {
	var state InstallState
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
func WriteInstallState(root string, state InstallState) error {
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

// RollbackInstall returns root's managed install to its previous record: verify
// proves that install is still usable (a backend runs its own probe), then the
// two records swap, so a second rollback undoes the first. Nothing is written
// when verify fails, which keeps a broken previous install from becoming the
// active one.
func RollbackInstall(ctx context.Context, root string, verify func(context.Context, InstallRecord) error) (InstallResult, error) {
	state, err := ReadInstallState(root)
	if err != nil {
		return InstallResult{}, err
	}
	if state.Previous == nil {
		return InstallResult{}, ErrNoPreviousInstall
	}
	restored := *state.Previous
	if err := verify(ctx, restored); err != nil {
		return InstallResult{}, fmt.Errorf("previous install %s is not usable: %w", restored.Version, err)
	}
	if err := WriteInstallState(root, InstallState{Active: &restored, Previous: state.Active}); err != nil {
		return InstallResult{}, err
	}
	return InstallResult{
		Version: restored.Version, Path: restored.Executable, Changed: true,
		Message: "rolled back to " + restored.Backend + " " + restored.Version,
	}, nil
}
