// Package config owns InferenceRig's neutral application configuration: the
// shared fields every backend relies on (listen address, storage/cache dirs,
// telemetry retention, startup services, security). Backend-specific settings
// live in per-profile YAML under ${INFERENCERIG_HOME}/profiles, not here.
package config

import (
	"bytes"
	"cmp"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ProjectName        = "inferencerig"
	ProjectDisplayName = "InferenceRig"
	ProjectHomeEnv     = "INFERENCERIG_HOME"
	ProjectConfigEnv   = "INFERENCERIG_CONFIG"
	ProjectHomeDirName = "." + ProjectName
	ProjectTokenEnv    = "INFERENCERIG_CONTROL_TOKEN"
	ProjectSocketEnv   = "INFERENCERIG_CONTROL_SOCKET"
	ProjectAppDirEnv   = "INFERENCERIG_APP_DIR"

	DefaultListenAddr          = "127.0.0.1:7000"
	DefaultAuthTokenEnv        = ProjectTokenEnv
	DefaultCatalogCacheTTL     = 6 * time.Hour
	DefaultLogArchiveRetention = 7 * 24 * time.Hour

	// StartupServiceControl is the internal Unix-socket control daemon.
	StartupServiceControl = "control"
	// StartupServiceWeb is the public HTTP/GUI/MCP gateway.
	StartupServiceWeb = "web"
)

// DefaultStartupServices starts both the control daemon and the web gateway.
func DefaultStartupServices() []string { return []string{StartupServiceControl, StartupServiceWeb} }

// Config is the neutral, backend-agnostic application configuration.
type Config struct {
	ListenAddr          string         `yaml:"listen_addr" json:"listen_addr"`
	ModelStorageDir     string         `yaml:"model_storage_dir" json:"model_storage_dir"`
	CatalogCacheDir     string         `yaml:"catalog_cache_dir" json:"catalog_cache_dir"`
	CatalogCacheTTL     time.Duration  `yaml:"catalog_cache_ttl" json:"catalog_cache_ttl"`
	LogArchiveRetention time.Duration  `yaml:"log_archive_retention" json:"log_archive_retention"`
	StartupServices     []string       `yaml:"startup_services" json:"startup_services,omitempty"`
	Security            SecurityConfig `yaml:"security" json:"security"`
}

type SecurityConfig struct {
	AuthTokenEnv       string `yaml:"auth_token_env" json:"auth_token_env"`
	DisableOriginCheck bool   `yaml:"disable_origin_check" json:"disable_origin_check"`
}

// Default returns the configuration used when no file is present.
func Default() Config {
	return Config{
		ListenAddr:          DefaultListenAddr,
		CatalogCacheTTL:     DefaultCatalogCacheTTL,
		LogArchiveRetention: DefaultLogArchiveRetention,
		StartupServices:     DefaultStartupServices(),
		Security:            SecurityConfig{AuthTokenEnv: DefaultAuthTokenEnv},
	}
}

// Load reads the configuration from ConfigPath().
func Load() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}
	return LoadFile(path)
}

// LoadFile reads and parses the configuration at path.
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes YAML over the defaults and validates the result.
func Parse(data []byte) (Config, error) {
	cfg := Default()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, err
	}
	cfg.applyDefaults()
	if cfg.LogArchiveRetention < 0 {
		return Config{}, fmt.Errorf("log_archive_retention must not be negative")
	}
	if err := ValidateStartupServices(cfg.StartupServices); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	c.ListenAddr = cmp.Or(c.ListenAddr, DefaultListenAddr)
	c.CatalogCacheTTL = cmp.Or(c.CatalogCacheTTL, DefaultCatalogCacheTTL)
	c.Security.AuthTokenEnv = cmp.Or(c.Security.AuthTokenEnv, DefaultAuthTokenEnv)
	if c.StartupServices == nil {
		c.StartupServices = DefaultStartupServices()
	}
}

// ValidateStartupServices rejects unknown startup service names.
func ValidateStartupServices(services []string) error {
	for _, name := range services {
		if name != StartupServiceControl && name != StartupServiceWeb {
			return fmt.Errorf("unknown startup service %q (want %q or %q)", name, StartupServiceControl, StartupServiceWeb)
		}
	}
	return nil
}

// AllowsNonLoopback reports whether ListenAddr binds beyond the local host,
// which exposes the gateway and warrants a bearer token.
func (c *Config) AllowsNonLoopback() bool {
	host, _, err := net.SplitHostPort(c.ListenAddr)
	if err != nil {
		if c.ListenAddr != "" && c.ListenAddr[0] == ':' {
			return true
		}
		host = c.ListenAddr
	}
	switch host {
	case "":
		return true
	case "localhost":
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return !ip.IsLoopback()
}

// Home resolves the InferenceRig home directory: ${INFERENCERIG_HOME} or
// ~/.inferencerig.
func Home() (string, error) {
	if home := os.Getenv(ProjectHomeEnv); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ProjectHomeDirName), nil
}

func homePath(sub ...string) (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home}, sub...)...), nil
}

// ConfigPath resolves the config file: ${INFERENCERIG_CONFIG} or
// ${home}/config.yaml.
func ConfigPath() (string, error) {
	if path := os.Getenv(ProjectConfigEnv); path != "" {
		return path, nil
	}
	return homePath("config.yaml")
}

func DefaultModelStorageDir() (string, error) { return homePath("models") }

func DefaultCatalogCacheDir() (string, error) { return homePath("cache", "hf-catalog") }

func ProfilesDir() (string, error) { return homePath("profiles") }

// GeneratedDir returns the directory holding a backend's generated (non
// user-owned) runtime files: ${home}/generated/<backend>. The backend name is
// a caller-supplied registry key; config carries no engine knowledge and only
// rejects a name that is not a single safe path element.
func GeneratedDir(backend string) (string, error) {
	if backend == "" {
		return "", fmt.Errorf("backend name is required")
	}
	if backend == "." || backend == ".." || strings.ContainsAny(backend, `/\`) {
		return "", fmt.Errorf("invalid backend name %q", backend)
	}
	return homePath("generated", backend)
}

// ControlSocketPath returns the Unix socket the control daemon listens on and
// that CLI/TUI clients dial.
func ControlSocketPath() (string, error) { return homePath("run", "control.sock") }

// ExpandHome expands a leading ~ to the user's home directory.
func ExpandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
