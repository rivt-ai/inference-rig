package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadOrDefaultFallsBackOnlyWhenAbsent is the runnable check on the
// fail-fast rule: a missing file is the single condition that may quietly use
// defaults, and every other failure must surface with the config path in it.
func TestLoadOrDefaultFallsBackOnlyWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	t.Setenv(ProjectConfigEnv, path)

	cfg, err := LoadOrDefault()
	if err != nil || cfg.ListenAddr != DefaultListenAddr {
		t.Fatalf("missing config = %q, %v; want defaults", cfg.ListenAddr, err)
	}

	for name, body := range map[string]string{
		"malformed yaml": "listen_addr: \"unterminated\n",
		"unknown key":    "listen_prot: 7000\n",
		"invalid value":  "log_archive_retention: -1h\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			err := errFrom(LoadOrDefault())
			if err == nil {
				t.Fatal("startup used defaults instead of failing")
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not name the config path", err)
			}
		})
	}

	t.Run("unreadable file", func(t *testing.T) {
		// A directory standing in for the file: an I/O error that is not
		// fs.ErrNotExist and that still reproduces when tests run as root,
		// where a 0000-mode file would remain readable.
		t.Setenv(ProjectConfigEnv, dir)
		if err := errFrom(LoadOrDefault()); err == nil {
			t.Fatal("an unreadable config must fail startup")
		}
	})
}

func errFrom(_ Config, err error) error { return err }

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse([]byte("listen_addr: \"127.0.0.1:9000\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:9000" {
		t.Errorf("listen_addr = %q", cfg.ListenAddr)
	}
	if cfg.CatalogCacheTTL != DefaultCatalogCacheTTL {
		t.Errorf("catalog_cache_ttl = %v, want default", cfg.CatalogCacheTTL)
	}
	if cfg.Security.AuthTokenEnv != DefaultAuthTokenEnv {
		t.Errorf("auth_token_env = %q, want default", cfg.Security.AuthTokenEnv)
	}
	if len(cfg.StartupServices) != 2 {
		t.Errorf("startup_services = %v, want both defaults", cfg.StartupServices)
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	if _, err := Parse([]byte("router: {port: 8080}\n")); err == nil {
		t.Fatal("expected unknown-field error for engine-specific key in neutral config")
	}
}

func TestParseRejectsUnknownStartupService(t *testing.T) {
	if _, err := Parse([]byte("startup_services: [control, bogus]\n")); err == nil {
		t.Fatal("expected error for unknown startup service")
	}
}

func TestParseRejectsNegativeRetention(t *testing.T) {
	if _, err := Parse([]byte("log_archive_retention: -1h\n")); err == nil {
		t.Fatal("expected error for negative log_archive_retention")
	}
}

func TestParseAllowsDisableAuthOnRemoteBind(t *testing.T) {
	// A non-loopback bind without auth is permitted and warned about rather
	// than rejected, so the setting must survive parsing intact.
	cfg, err := Parse([]byte("listen_addr: \"0.0.0.0:7000\"\nsecurity: {disable_auth: true}\n"))
	if err != nil || !cfg.Security.DisableAuth {
		t.Fatalf("remote disable_auth = %v, %v", cfg.Security.DisableAuth, err)
	}
	cfg, err = Parse([]byte("listen_addr: \"127.0.0.1:7000\"\nsecurity: {disable_auth: true}\n"))
	if err != nil || !cfg.Security.DisableAuth {
		t.Fatalf("loopback disable_auth = %v, %v", cfg.Security.DisableAuth, err)
	}
}

// TestWarnIfExposedOnlyWhenExposed is the runnable check on the warn condition:
// it must fire for disabled auth on a remote bind and stay silent otherwise.
func TestWarnIfExposedOnlyWhenExposed(t *testing.T) {
	cases := []struct {
		addr    string
		disable bool
		want    bool
	}{
		{"0.0.0.0:7000", true, true},
		{"192.168.1.5:7000", true, true},
		{"0.0.0.0:7000", false, false},
		{"127.0.0.1:7000", true, false},
		{"localhost:7000", true, false},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		cfg := &Config{ListenAddr: tc.addr, Security: SecurityConfig{DisableAuth: tc.disable}}
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
		cfg.WarnIfExposed()
		slog.SetDefault(prev)
		if got := strings.Contains(buf.String(), "without authentication"); got != tc.want {
			t.Errorf("WarnIfExposed(%q, disable=%v) warned = %v, want %v", tc.addr, tc.disable, got, tc.want)
		}
	}
}

func TestAllowsNonLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:7000": false,
		"localhost:7000": false,
		"[::1]:7000":     false,
		"0.0.0.0:7000":   true,
		":7000":          true,
		"192.168.1.5:70": true,
		// Bare IPv6 literals do not parse as host:port; they must still be
		// classified by address, not treated as a wildcard bind.
		"::1": false,
		"::":  true,
	}
	for addr, want := range cases {
		cfg := Config{ListenAddr: addr}
		if got := cfg.AllowsNonLoopback(); got != want {
			t.Errorf("AllowsNonLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestHomeHonorsEnv(t *testing.T) {
	t.Setenv(ProjectHomeEnv, "/tmp/rig-home")
	home, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	if home != "/tmp/rig-home" {
		t.Errorf("Home() = %q", home)
	}
	t.Setenv(ProjectConfigEnv, "")
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/rig-home/config.yaml" {
		t.Errorf("ConfigPath() = %q", path)
	}
}

func TestGeneratedDir(t *testing.T) {
	t.Setenv(ProjectHomeEnv, "/tmp/rig-home")
	dir, err := GeneratedDir("sample")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/tmp/rig-home/generated/sample" {
		t.Errorf("GeneratedDir() = %q", dir)
	}
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`} {
		if _, err := GeneratedDir(bad); err == nil {
			t.Errorf("GeneratedDir(%q) accepted an unsafe backend name", bad)
		}
	}
}
