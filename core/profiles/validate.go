package profiles

import (
	"fmt"
	"regexp"

	"inferencerig/config"
)

var safeNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidateName rejects empty, dot, and unsafe profile names.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: profile name is required", ErrInvalid)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: invalid profile name %q", ErrInvalid, name)
	}
	if !safeNamePattern.MatchString(name) {
		return fmt.Errorf("%w: invalid profile name %q", ErrInvalid, name)
	}
	return nil
}

// normalizeCommon validates and normalizes the neutral common fields, filling
// in the profile name from the directory when omitted. Backend-specific
// validation of engine_args is NOT done here — it is the BackendValidator's
// job. Shared code carries no engine defaults, so listen host/port and any
// executable are never filled in here — a profile must specify a valid port.
func normalizeCommon(name string, parsed Profile) (Profile, error) {
	effective := parsed
	if effective.Name == "" {
		effective.Name = name
	} else if effective.Name != name {
		return Profile{}, fmt.Errorf("%w: profile name %q must match profile %q", ErrInvalid, effective.Name, name)
	}
	if isEmptyVersion(effective.Version) {
		return Profile{}, fmt.Errorf("%w: version is required", ErrInvalid)
	}
	if effective.Backend == "" {
		return Profile{}, fmt.Errorf("%w: backend is required", ErrInvalid)
	}
	if effective.Listen.Port < 1 || effective.Listen.Port > 65535 {
		return Profile{}, fmt.Errorf("%w: invalid listen port %d", ErrInvalid, effective.Listen.Port)
	}
	if effective.Model.Source == "" {
		return Profile{}, fmt.Errorf("%w: model.source is required", ErrInvalid)
	}
	effective.Model.Source = config.ExpandHome(effective.Model.Source)
	return effective, nil
}

// isEmptyVersion reports whether a decoded version field is absent. A missing
// key decodes to nil; an explicit empty string is also treated as absent.
func isEmptyVersion(version any) bool {
	if version == nil {
		return true
	}
	s, ok := version.(string)
	return ok && s == ""
}
