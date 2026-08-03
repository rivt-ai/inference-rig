// Package config owns InferenceRig's neutral application configuration: the
// shared fields every backend relies on (listen address, storage/cache dirs,
// telemetry retention, startup services, security). Backend-specific settings
// live in per-profile YAML under ${INFERENCERIG_HOME}/profiles, not here.
package config

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
	// CommandName is what the user types. It is deliberately separate from
	// ProjectName: ProjectName still names the home directory, PID files, log
	// files and the user service, so shortening the command cannot move an
	// existing installation's data.
	CommandName        = "infr"
	ProjectHomeEnv     = "INFERENCERIG_HOME"
	ProjectConfigEnv   = "INFERENCERIG_CONFIG"
	ProjectHomeDirName = "." + ProjectName
	ProjectTokenEnv    = "INFERENCERIG_CONTROL_TOKEN"
	ProjectSocketEnv   = "INFERENCERIG_CONTROL_SOCKET"
	ProjectAppDirEnv   = "INFERENCERIG_APP_DIR"
	// NoUpdateCheckEnv, when non-empty, stops the daemon contacting GitHub to
	// learn whether a newer release exists.
	NoUpdateCheckEnv = "INFERENCERIG_NO_UPDATE_CHECK"

	DefaultListenAddr          = "127.0.0.1:7000"
	DefaultAuthTokenEnv        = ProjectTokenEnv
	DefaultCatalogCacheTTL     = 6 * time.Hour
	DefaultLogArchiveRetention = 7 * 24 * time.Hour

	// StartupServiceControl is the internal Unix-socket control daemon.
	StartupServiceControl = "control"
	// StartupServiceWeb is the public HTTP/GUI/MCP gateway.
	StartupServiceWeb = "web"

	// LogServiceControl names the control daemon's own log. The daemon is
	// detached under ProjectName, and a service log is named after the process
	// that writes it, so the two must stay equal.
	LogServiceControl = ProjectName
	// LogServiceEngine names the supervised backend runtime's log. Engine
	// output is kept out of LogServiceControl so each stream stays readable on
	// its own terms: structured records here, raw engine chatter there.
	LogServiceEngine = "engine"
)

// DefaultStartupServices starts both the control daemon and the web gateway.
func DefaultStartupServices() []string { return []string{StartupServiceControl, StartupServiceWeb} }

// ServiceProcessName maps a startup service to its detached process name. The
// control daemon runs under the project name rather than "control", because its
// PID and log files predate the startup-service naming.
func ServiceProcessName(service string) string {
	if service == StartupServiceControl {
		return ProjectName
	}
	return service
}

// ServiceArgs returns the subcommand that runs a startup service.
func ServiceArgs(service string) []string {
	return map[string][]string{
		StartupServiceControl: {"serve"},
		StartupServiceWeb:     {"web"},
	}[service]
}

// Config is the neutral, backend-agnostic application configuration.
type Config struct {
	ListenAddr          string         `yaml:"listen_addr" json:"listen_addr"`
	ModelStorageDir     string         `yaml:"model_storage_dir" json:"model_storage_dir"`
	CatalogCacheDir     string         `yaml:"catalog_cache_dir" json:"catalog_cache_dir"`
	CatalogCacheTTL     time.Duration  `yaml:"catalog_cache_ttl" json:"catalog_cache_ttl"`
	LogArchiveRetention time.Duration  `yaml:"log_archive_retention" json:"log_archive_retention"`
	StartupServices     []string       `yaml:"startup_services" json:"startup_services,omitempty"`
	AutostartProfiles   []string       `yaml:"autostart_profiles" json:"autostart_profiles,omitempty"`
	Security            SecurityConfig `yaml:"security" json:"security"`
}

type SecurityConfig struct {
	AuthTokenEnv       string `yaml:"auth_token_env" json:"auth_token_env"`
	DisableOriginCheck bool   `yaml:"disable_origin_check" json:"disable_origin_check"`
	// AllowedOrigins lists the browser origins permitted to reach the gateway.
	// Empty means loopback-only, which is the default posture. It is a list
	// rather than a single origin because one install is commonly reached by
	// both a hostname and an IP.
	AllowedOrigins []string `yaml:"allowed_origins" json:"allowed_origins,omitempty"`
	// DisableAuth drops the bearer-token guard entirely so a single-user local
	// install can drive the gateway without pasting a token. On a bind that
	// reaches the network it is refused unless AllowExposedWithoutAuth is also
	// set: two deliberate keys, so nobody publishes every RPC by pasting one
	// config snippet.
	DisableAuth bool `yaml:"disable_auth" json:"disable_auth"`
	// AllowExposedWithoutAuth is the second key that permits DisableAuth on a
	// non-loopback bind, for a deployment that terminates authentication in a
	// reverse proxy in front of the gateway.
	AllowExposedWithoutAuth bool `yaml:"allow_exposed_without_auth" json:"allow_exposed_without_auth"`
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

// LoadOrDefault is what every entry point should call. It returns the defaults
// only when no config file exists; a syntax error, an unknown key, a failed
// validation or an unreadable file is returned instead, so a machine the
// operator believes is configured never silently serves the defaults with its
// security settings, paths and listen address reverted.
func LoadOrDefault() (Config, error) {
	cfg, err := Load()
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	return cfg, err
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
	if err := cfg.ValidateSecurity(); err != nil {
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

// ErrExposedWithoutAuth reports the one config failure that has known, named
// remedies. It is wrapped rather than formatted into the message alone so a
// diagnostic can offer those remedies via errors.Is instead of matching the
// error text, which would silently stop working the moment the wording changes.
var ErrExposedWithoutAuth = errors.New("authentication disabled on a bind that reaches the network")

// ValidateSecurity checks security settings against the bind address. Disabling
// auth on a bind that reaches the network publishes every RPC to anything that
// can route to the port, so it is refused unless the operator also sets
// security.allow_exposed_without_auth — a second, differently named key that a
// pasted config snippet will not carry by accident.
func (c *Config) ValidateSecurity() error {
	if c.Security.DisableAuth && c.AllowsNonLoopback() && !c.Security.AllowExposedWithoutAuth {
		return fmt.Errorf("%w: security.disable_auth with a non-loopback listen_addr %q publishes every RPC unauthenticated; "+
			"bind loopback, drop security.disable_auth, or set security.allow_exposed_without_auth: true",
			ErrExposedWithoutAuth, c.ListenAddr)
	}
	c.WarnIfExposed()
	return nil
}

// WarnIfExposed logs a warning when auth is disabled on a non-loopback bind.
// Safe to call repeatedly; it is the single place that decides the posture is
// unsafe, so every entry point that loads config inherits the warning.
func (c *Config) WarnIfExposed() {
	if c.Security.DisableAuth && c.AllowsNonLoopback() {
		slog.Warn("serving without authentication on a non-loopback address",
			"listen_addr", c.ListenAddr, "setting", "security.disable_auth")
	}
}

// BrowseURL is the address to point a browser at for the given listen address.
// A wildcard bind is not a reachable host, so it resolves to loopback: that is
// the interface the operator reading the message is sitting on.
func BrowseURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		host = listenAddr
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if port == "" {
		return "http://" + host
	}
	return "http://" + net.JoinHostPort(host, port)
}

// AllowsNonLoopback reports whether ListenAddr binds beyond the local host,
// which exposes the gateway and warrants a bearer token.
func (c *Config) AllowsNonLoopback() bool {
	// A ':'-leading address such as ":7000" parses cleanly with an empty host,
	// which the switch below already treats as a wildcard bind. Addresses that
	// fail to parse fall through to ParseIP, so a bare IPv6 literal like "::1"
	// is still recognized as loopback.
	host, _, err := net.SplitHostPort(c.ListenAddr)
	if err != nil {
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

// Paths are the resolved InferenceRig locations. Every individual resolver
// fails only when the home directory cannot be resolved, so callers needing
// several of them get one error instead of repeating the same check per path.
type Paths struct {
	Home          string
	Config        string
	Profiles      string
	CatalogCache  string
	ModelStorage  string
	ControlSocket string
	DownloadState string
}

// ResolvePaths resolves every standard location in one step.
func ResolvePaths() (Paths, error) {
	home, err := Home()
	if err != nil {
		return Paths{}, err
	}
	configPath, err := ConfigPath()
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		Home:          home,
		Config:        configPath,
		Profiles:      filepath.Join(home, "profiles"),
		CatalogCache:  filepath.Join(home, "cache", "hf-catalog"),
		ModelStorage:  filepath.Join(home, "models"),
		ControlSocket: filepath.Join(home, "run", "control.sock"),
		DownloadState: filepath.Join(home, "state", "downloads"),
	}, nil
}

// ControlSocketPath returns the Unix socket the control daemon listens on and
// that CLI/TUI clients dial.
func ControlSocketPath() (string, error) { return homePath("run", "control.sock") }

// GatewayTokenPath returns the file the gateway token persists to. With reads
// authenticated, a token that only lived for one run would leave the web UI
// blank after every restart, so it survives on disk beside the control socket
// under the same 0700 run directory.
func GatewayTokenPath() (string, error) { return homePath("run", "gateway.token") }

// ExpandHome expands a leading ~ to the user's home directory.
func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	// filepath.Join ignores an empty element, so path[1:] covers both "~" and
	// "~/sub" without a second case.
	return filepath.Join(home, path[1:])
}

// FailureJournalPath returns the file recording failed control-plane
// operations. It sits under the home directory rather than run/ because it
// must survive a daemon restart: a diagnostic reads it precisely when the
// daemon that would have served the in-memory event history is not running.
func FailureJournalPath() (string, error) { return homePath("state", "failures.jsonl") }
