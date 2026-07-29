package setup

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"inferencerig/config"
	"inferencerig/platform/filedoc"
)

func defaultAnswers(paths Paths, backend string) Answers {
	return Answers{
		ListenAddr: config.DefaultListenAddr, AuthTokenEnv: config.DefaultAuthTokenEnv,
		ModelStorageDir: paths.DefaultModelStorage, Backend: backend,
		StartupServices: config.DefaultStartupServices(),
	}
}

// write creates the home directory, the model store, and the config file.
// No profile is written: the TUI and web UI create profiles against a running
// daemon, where the catalog is browsable and a model can be downloaded in the
// same step.
func (w *Wizard) write(paths Paths, answers Answers, force bool) (filedoc.WriteResult, error) {
	content, err := renderConfig(paths, answers)
	if err != nil {
		return filedoc.WriteResult{}, err
	}
	if err := os.MkdirAll(paths.Home, 0o700); err != nil {
		return filedoc.WriteResult{}, fmt.Errorf("create application home: %w", err)
	}
	if err := os.Chmod(paths.Home, 0o700); err != nil {
		return filedoc.WriteResult{}, fmt.Errorf("secure application home: %w", err)
	}
	if err := os.MkdirAll(answers.ModelStorageDir, 0o700); err != nil {
		return filedoc.WriteResult{}, fmt.Errorf("create model storage: %w", err)
	}
	exists, err := pathExists(paths.Config)
	if err != nil {
		return filedoc.WriteResult{}, err
	}
	if force && exists {
		return filedoc.WriteFile(paths.Config, content, filedoc.WriteOptions{Perm: 0o600, Backup: true})
	}
	if exists {
		return filedoc.WriteResult{}, fmt.Errorf("config already exists at %s", paths.Config)
	}
	if err := filedoc.AtomicCreate(paths.Config, []byte(content), 0o600); err != nil {
		return filedoc.WriteResult{}, err
	}
	return filedoc.WriteResult{
		Path: paths.Config, SizeBytes: int64(len(content)),
		SHA256: filedoc.SHA256Hex([]byte(content)),
	}, nil
}

func renderConfig(paths Paths, answers Answers) (string, error) {
	cfg := config.Default()
	cfg.ListenAddr = answers.ListenAddr
	cfg.ModelStorageDir = answers.ModelStorageDir
	cfg.CatalogCacheDir = paths.DefaultCatalogCache
	cfg.StartupServices = answers.StartupServices
	cfg.AutostartProfiles = nil
	cfg.Security.AuthTokenEnv = answers.AuthTokenEnv
	cfg.Security.DisableAuth = answers.DisableAuth
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	if _, err := config.Parse(data); err != nil {
		return "", fmt.Errorf("validate generated config: %w", err)
	}
	return string(data), nil
}
