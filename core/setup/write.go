package setup

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"inferencerig/config"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/platform/filedoc"
)

func defaultAnswers(paths Paths, backend string) Answers {
	return Answers{
		ListenAddr: config.DefaultListenAddr, AuthTokenEnv: config.DefaultAuthTokenEnv,
		ModelStorageDir: paths.DefaultModelStorage, ProfileName: "default",
		Backend: backend, Host: "127.0.0.1", Port: 8080,
		StartupServices: config.DefaultStartupServices(),
	}
}

func (w *Wizard) write(ctx context.Context, paths Paths, answers Answers, force bool) (*controlv1.Profile, filedoc.WriteResult, error) {
	content, err := renderConfig(paths, answers)
	if err != nil {
		return nil, filedoc.WriteResult{}, err
	}
	if err := os.MkdirAll(paths.Home, 0o700); err != nil {
		return nil, filedoc.WriteResult{}, fmt.Errorf("create application home: %w", err)
	}
	if err := os.Chmod(paths.Home, 0o700); err != nil {
		return nil, filedoc.WriteResult{}, fmt.Errorf("secure application home: %w", err)
	}
	if err := os.MkdirAll(answers.ModelStorageDir, 0o700); err != nil {
		return nil, filedoc.WriteResult{}, fmt.Errorf("create model storage: %w", err)
	}
	profile, err := w.putProfile(ctx, answers, !force)
	if err != nil {
		return nil, filedoc.WriteResult{}, err
	}
	exists, err := pathExists(paths.Config)
	if err != nil {
		return nil, filedoc.WriteResult{}, err
	}
	if force && exists {
		result, err := filedoc.WriteFile(paths.Config, content, filedoc.WriteOptions{Perm: 0o600, Backup: true})
		return profile, result, err
	}
	if exists {
		return nil, filedoc.WriteResult{}, fmt.Errorf("config already exists at %s", paths.Config)
	}
	if err := filedoc.AtomicCreate(paths.Config, []byte(content), 0o600); err != nil {
		return profile, filedoc.WriteResult{}, err
	}
	result := filedoc.WriteResult{
		Path: paths.Config, SizeBytes: int64(len(content)),
		SHA256: filedoc.SHA256Hex([]byte(content)),
	}
	return profile, result, nil
}

func renderConfig(paths Paths, answers Answers) (string, error) {
	cfg := config.Default()
	cfg.ListenAddr = answers.ListenAddr
	cfg.ModelStorageDir = answers.ModelStorageDir
	cfg.CatalogCacheDir = paths.DefaultCatalogCache
	cfg.StartupServices = answers.StartupServices
	cfg.AutostartProfiles = nil
	if answers.Autostart {
		cfg.AutostartProfiles = []string{answers.ProfileName}
	}
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
