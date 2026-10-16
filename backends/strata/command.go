package strata

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"inferencerig/config"
	"inferencerig/core/profiles"
)

var (
	errMissingModel     = errors.New("model source is required")
	errNotInstalled     = errors.New("strata is not installed; run the backend install first")
	errSetupOwnsModels  = errors.New("strata models are managed by Strata's setup")
	errUnsafeSourceName = errors.New("model source must be a Strata size name or a strata-*.json path")
)

// Command is a rendered Strata invocation.
type Command struct {
	Executable string
	Argv       []string
	Env        map[string]string
	Display    string
	// Setup marks a run through setup.py, which may download and prepare the
	// model before serving, so readiness can take far longer.
	Setup bool
}

// configSource reports whether source names a setup-written engine config
// rather than a model size. Setup writes that file into the checkout root, so
// the checkout is the file's directory and no read is needed to find it.
func configSource(source string) bool {
	return strings.EqualFold(filepath.Ext(source), ".json")
}

// buildCommand renders the profile into either a direct server run (config
// source) or a setup run (size name) through the managed checkout at
// checkout. Whether a checkout is installed is the launcher's concern, so a
// profile validates before the engine is installed.
func buildCommand(checkout string, p profiles.Profile) (Command, error) {
	source := strings.TrimSpace(p.Model.Source)
	if source == "" {
		return Command{}, errMissingModel
	}
	if configSource(source) {
		return serverCommand(config.ExpandHome(source), p)
	}
	if strings.HasPrefix(source, "-") || strings.ContainsAny(source, " \t\r\n") {
		return Command{}, errUnsafeSourceName
	}
	argv := []string{filepath.Join(checkout, "setup.py"), "--model", source, "--yes", "--no-browser"}
	return finish(venvPython(checkout), argv, p, setupReserved, true)
}

func serverCommand(cfg string, p profiles.Profile) (Command, error) {
	checkout := filepath.Dir(cfg)
	argv := []string{filepath.Join(checkout, "serve", "server.py"), "--engine", "strata", "--config", cfg}
	return finish(venvPython(checkout), argv, p, serverReserved, false)
}

func finish(python string, argv []string, p profiles.Profile, reserved []string, setup bool) (Command, error) {
	if p.Listen.Host != "" {
		argv = append(argv, "--host", p.Listen.Host)
	}
	if p.Listen.Port > 0 {
		argv = append(argv, "--port", strconv.Itoa(p.Listen.Port))
	}
	extra, err := renderArgs(p.EngineArgs, reserved)
	if err != nil {
		return Command{}, err
	}
	argv = append(argv, extra...)
	cmd := Command{
		Executable: python, Argv: argv, Setup: setup,
		Display: strings.Join(append([]string{python}, argv...), " "),
	}
	if setup {
		// setup.py otherwise spawns the server as a child; exec keeps one
		// process for the supervisor to signal and the engine to shut down.
		cmd.Env = map[string]string{"STRATA_EXECV": "1"}
	}
	return cmd, nil
}

func venvPython(checkout string) string {
	return filepath.Join(checkout, ".venv", "bin", "python")
}

// Keys the profile already owns, or that would make a supervised run
// interactive or detached.
var (
	serverReserved = []string{"engine", "config", "host", "port", "open"}
	setupReserved  = []string{
		"model", "host", "port", "yes", "browser", "no-browser", "setup", "no-start",
		"check", "update", "rollback-engine", "inspect",
	}
)

func renderArgs(args map[string]any, reserved []string) ([]string, error) {
	var out []string
	for _, key := range slices.Sorted(maps.Keys(args)) {
		flag, err := strataFlag(key, reserved)
		if err != nil {
			return nil, err
		}
		values, ok := args[key].([]any)
		if !ok {
			values = []any{args[key]}
		}
		for _, value := range values {
			rendered, err := renderArg(key, flag, value)
			if err != nil {
				return nil, err
			}
			out = append(out, rendered...)
		}
	}
	return out, nil
}

func strataFlag(key string, reserved []string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "-") || strings.ContainsAny(key, " \t\r\n") {
		return "", fmt.Errorf("engine_args contains invalid key %q", key)
	}
	if slices.Contains(reserved, key) {
		return "", fmt.Errorf("engine_args.%s is reserved by the strata backend", key)
	}
	return "--" + key, nil
}

// renderArg renders one value. Strata's flags are argparse store_true
// switches, so false omits the flag rather than negating it.
func renderArg(key, flag string, value any) ([]string, error) {
	switch typed := value.(type) {
	case bool:
		if typed {
			return []string{flag}, nil
		}
		return nil, nil
	case string:
		return []string{flag, config.ExpandHome(typed)}, nil
	}
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return nil, fmt.Errorf("engine_args.%s is null", key)
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return []string{flag, strconv.FormatInt(v.Int(), 10)}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return []string{flag, strconv.FormatUint(v.Uint(), 10)}, nil
	case reflect.Float32, reflect.Float64:
		return []string{flag, strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits())}, nil
	default:
		return nil, fmt.Errorf("engine_args.%s has unsupported type %T", key, value)
	}
}
