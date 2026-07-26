package mlx

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"inferencerig/config"
	"inferencerig/core/profiles"
)

var errMissingModel = errors.New("model source is required")

// Command is a rendered mlx_lm server invocation.
type Command struct {
	Executable string
	Argv       []string
	Display    string
}

func buildCommand(executable string, p profiles.Profile) (Command, error) {
	if p.Model.Source == "" {
		return Command{}, errMissingModel
	}
	argv := []string{"-m", "mlx_lm", "server", "--model", config.ExpandHome(p.Model.Source)}
	if p.Listen.Host != "" {
		argv = append(argv, "--host", p.Listen.Host)
	}
	if p.Listen.Port > 0 {
		argv = append(argv, "--port", strconv.Itoa(p.Listen.Port))
	}
	extra, err := renderArgs(p.EngineArgs)
	if err != nil {
		return Command{}, err
	}
	argv = append(argv, extra...)
	return Command{
		Executable: executable,
		Argv:       argv,
		Display:    strings.Join(append([]string{executable}, argv...), " "),
	}, nil
}

func renderArgs(args map[string]any) ([]string, error) {
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out []string
	for _, key := range keys {
		flag, err := mlxFlag(key)
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

func mlxFlag(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "-") || strings.ContainsAny(key, " \t\r\n") {
		return "", fmt.Errorf("engine_args contains invalid key %q", key)
	}
	switch key {
	case "model", "host", "port":
		return "", fmt.Errorf("engine_args.%s conflicts with a profile field", key)
	default:
		return "--" + key, nil
	}
}

func renderArg(key, flag string, value any) ([]string, error) {
	if boolean, ok := value.(bool); ok {
		if boolean {
			return []string{flag}, nil
		}
		return []string{"--no-" + strings.TrimPrefix(flag, "--")}, nil
	}
	if text, ok := value.(string); ok {
		return []string{flag, config.ExpandHome(text)}, nil
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
