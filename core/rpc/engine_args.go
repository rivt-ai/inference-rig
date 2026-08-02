package rpc

import (
	"fmt"
	"math"

	"gopkg.in/yaml.v3"

	"inferencerig/core/control"
	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
)

// normalizeEngineArgs coerces YAML-decoded values into the types structpb
// accepts. yaml.v3 yields int and map[string]any, neither of which structpb
// handles, so an unconverted profile would silently lose its engine args.
func normalizeEngineArgs(args map[string]any) map[string]any {
	normalized, _ := mapContainer(args, normalizeEngineValue).(map[string]any)
	return normalized
}

func normalizeEngineValue(value any) any {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint64:
		return float64(typed)
	case map[any]any:
		// yaml.v3 produces this for nested mappings with non-string keys.
		nested := make(map[string]any, len(typed))
		for key, item := range typed {
			nested[fmt.Sprint(key)] = item
		}
		return normalizeEngineArgs(nested)
	default:
		return mapContainer(value, normalizeEngineValue)
	}
}

// mapContainer applies convert to every element of a slice or string-keyed map
// and returns anything else untouched. Both engine-arg conversions below walk
// the same YAML/structpb shapes; only their scalar rule differs, so the
// recursion lives here once rather than being written out per conversion.
func mapContainer(value any, convert func(any) any) any {
	switch typed := value.(type) {
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, convert(item))
		}
		return items
	case map[string]any:
		nested := make(map[string]any, len(typed))
		for key, item := range typed {
			nested[key] = convert(item)
		}
		return nested
	default:
		return value
	}
}

// renderProfileYAML turns a structured profile into the canonical YAML the
// store validates. Integral engine-arg values are emitted as integers: JSON and
// structpb have only float64, and a rendered "threads: 8.0" is not a value any
// engine accepts.
func renderProfileYAML(name string, message *controlv1.Profile) (string, error) {
	profile := profiles.Profile{
		Version: 1,
		Name:    name,
		Backend: message.GetBackend(),
		Model: profiles.ModelSpec{
			Source: message.GetModelSource(), Reference: message.GetModelReference(),
		},
		Listen: profiles.ListenSpec{Host: message.GetHost(), Port: int(message.GetPort())},
	}
	if args := message.GetEngineArgs(); args != nil {
		profile.EngineArgs = make(map[string]any, len(args.GetFields()))
		for key, value := range args.AsMap() {
			profile.EngineArgs[key] = demoteWholeFloats(value)
		}
	}
	rendered, err := yaml.Marshal(profile)
	if err != nil {
		return "", control.Errorf(control.ErrorInvalidInput, "render profile: %v", err)
	}
	return string(rendered), nil
}

func demoteWholeFloats(value any) any {
	switch typed := value.(type) {
	case float64:
		if typed == math.Trunc(typed) && math.Abs(typed) < math.MaxInt64 {
			return int64(typed)
		}
		return typed
	default:
		return mapContainer(value, demoteWholeFloats)
	}
}
