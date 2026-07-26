package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/term"

	"inferencerig/config"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/platform/filedoc"
)

type Paths struct {
	Home, Config, ProfilesDir, DefaultModelStorage, DefaultCatalogCache string
}

type Result struct {
	Skipped     bool
	Profile     *controlv1.Profile
	ConfigWrite filedoc.WriteResult
}

func ResolvePaths() (Paths, error) {
	home, err := config.Home()
	if err != nil {
		return Paths{}, err
	}
	configPath, err := config.ConfigPath()
	if err != nil {
		return Paths{}, err
	}
	profilesDir, err := config.ProfilesDir()
	if err != nil {
		return Paths{}, err
	}
	models, err := config.DefaultModelStorageDir()
	if err != nil {
		return Paths{}, err
	}
	cache, err := config.DefaultCatalogCacheDir()
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		Home: home, Config: configPath, ProfilesDir: profilesDir,
		DefaultModelStorage: models, DefaultCatalogCache: cache,
	}, nil
}

func (w *Wizard) Ensure(ctx context.Context, input io.Reader, output io.Writer) (Result, error) {
	return w.run(ctx, input, output, false)
}

func (w *Wizard) Rerun(ctx context.Context, input io.Reader, output io.Writer) (Result, error) {
	return w.run(ctx, input, output, true)
}

func (w *Wizard) run(ctx context.Context, input io.Reader, output io.Writer, force bool) (Result, error) {
	if os.Getenv(config.ProjectConfigEnv) != "" {
		return Result{Skipped: true}, nil
	}
	paths, err := ResolvePaths()
	if err != nil {
		return Result{}, err
	}
	exists, err := pathExists(paths.Config)
	if err != nil {
		return Result{}, err
	}
	if exists && !force {
		return Result{Skipped: true}, nil
	}
	if !interactive(input, output) {
		return Result{}, fmt.Errorf(
			"no %s config found at %s; run `%s setup` in an interactive terminal",
			config.ProjectDisplayName, paths.Config, config.ProjectName,
		)
	}
	if exists {
		_, _ = fmt.Fprintf(output,
			"A %s config already exists at %s — continuing will overwrite it and keep a backup.\n\n",
			config.ProjectDisplayName, paths.Config,
		)
	}
	answers, err := w.collect(ctx, paths, input, output, force)
	if err != nil {
		return Result{}, err
	}
	profile, configWrite, err := w.write(ctx, paths, answers, force)
	if err != nil {
		return Result{}, err
	}
	printSummary(output, paths, answers, configWrite)
	return Result{Profile: profile, ConfigWrite: configWrite}, nil
}

func interactive(input io.Reader, output io.Writer) bool {
	in, inOK := input.(interface{ Fd() uintptr })
	out, outOK := output.(interface{ Fd() uintptr })
	return inOK && outOK && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("stat %q: %w", path, err)
}

func printSummary(output io.Writer, paths Paths, answers Answers, write filedoc.WriteResult) {
	_, _ = fmt.Fprintf(output,
		"%s setup complete.\n\nCreated:\n  %s\n  %s\n\nNext steps:\n"+
			"  1. Open `%s` to use the TUI\n"+
			"  2. Download or select a model for profile %q\n"+
			"  3. Start the profile from the TUI, CLI, or web interface\n",
		config.ProjectDisplayName, paths.Config,
		filepath.Join(paths.ProfilesDir, answers.ProfileName, "profile.yaml"),
		config.ProjectName, answers.ProfileName,
	)
	if write.BackupPath != "" {
		_, _ = fmt.Fprintf(output, "\nPrevious config backup: %s\n", write.BackupPath)
	}
}
