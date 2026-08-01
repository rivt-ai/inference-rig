package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"inferencerig/config"
	"inferencerig/platform/filedoc"
)

// permissionRule is one path and the mode it is written with. Everything in
// InferenceRig enforces these at write time; nothing reads them back, so a
// mode loosened by hand, a restored backup or a permissive umask goes
// unnoticed until it matters.
type permissionRule struct {
	path string
	want os.FileMode
	dir  bool
}

func permissionRules(paths config.Paths) []permissionRule {
	run := filepath.Dir(paths.ControlSocket)
	return []permissionRule{
		{path: paths.Home, want: 0o700, dir: true},
		{path: paths.Config, want: 0o600},
		{path: run, want: 0o700, dir: true},
		{path: paths.ControlSocket, want: 0o600},
		{path: filepath.Join(run, "gateway.token"), want: 0o600},
		{path: paths.Profiles, want: 0o700, dir: true},
	}
}

func checkPermissions(_ context.Context, e *env) Check {
	const id, title = "files.permissions", "file permissions"
	var problems []string
	worst := StatusOK
	for _, rule := range permissionRules(e.paths) {
		status, problem := evaluatePermission(rule)
		if problem != "" {
			problems = append(problems, problem)
		}
		if status == StatusFail || (status == StatusWarn && worst == StatusOK) {
			worst = status
		}
	}
	if symlink := checkSymlinks(e.paths); symlink != "" {
		problems = append(problems, symlink)
		worst = StatusFail
	}
	if worst == StatusOK {
		return ok(id, title, "home, config and run directory are private")
	}
	sort.Strings(problems)
	summary := fmt.Sprintf("%d of %d paths are more permissive than intended",
		len(problems), len(permissionRules(e.paths)))
	return Check{ID: id, Title: title, Status: worst, Summary: summary}.
		withDetail(strings.Join(problems, "\n"))
}

// evaluatePermission reports how a path's mode differs from what wrote it.
// World or group write is a failure; merely readable is a warning, since the
// token and socket are the only entries whose contents are secret.
func evaluatePermission(rule permissionRule) (Status, string) {
	info, err := os.Stat(rule.path)
	if err != nil {
		return StatusOK, "" // absent is not a permission problem
	}
	mode := info.Mode().Perm()
	if mode == rule.want {
		return StatusOK, ""
	}
	extra := mode &^ rule.want
	if extra == 0 {
		return StatusOK, ""
	}
	problem := fmt.Sprintf("%s is %04o, expected %04o", rule.path, mode, rule.want)
	if extra&0o022 != 0 {
		return StatusFail, problem + " (writable by others)"
	}
	return StatusWarn, problem
}

// checkSymlinks rejects a config file, or any directory above it, that is a
// symlink — the same rule filedoc enforces before writing, applied as a read.
func checkSymlinks(paths config.Paths) string {
	if err := filedoc.RejectSymlink(paths.Config); err != nil && !isMissing(err) {
		return paths.Config + ": " + err.Error()
	}
	if err := filedoc.RejectSymlinkAncestors(paths.Config); err != nil && !isMissing(err) {
		return "a directory above the config file is a symlink: " + err.Error()
	}
	return ""
}

func isMissing(err error) bool { return errors.Is(err, fs.ErrNotExist) }
