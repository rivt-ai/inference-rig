package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"inferencerig/config"
	"inferencerig/platform/filedoc"
)

type Paths struct {
	Home, Config, ProfilesDir, DefaultModelStorage, DefaultCatalogCache string
}

type Result struct {
	Skipped     bool
	ConfigWrite filedoc.WriteResult
}

func ResolvePaths() (Paths, error) {
	p, err := config.ResolvePaths()
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		Home: p.Home, Config: p.Config, ProfilesDir: p.Profiles,
		DefaultModelStorage: p.ModelStorage, DefaultCatalogCache: p.CatalogCache,
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
	answers, err := w.collect(ctx, paths, input, output)
	if err != nil {
		return Result{}, err
	}
	configWrite, err := w.write(paths, answers, force)
	if err != nil {
		return Result{}, err
	}
	printSummary(output, paths, configWrite)
	return Result{ConfigWrite: configWrite}, nil
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

func printSummary(output io.Writer, paths Paths, write filedoc.WriteResult) {
	_, _ = fmt.Fprintf(output,
		"%s setup complete.\n\nCreated:\n  %s\n\nNo profile exists yet. Next steps:\n"+
			"  1. Open `%s` to use the TUI, or the web interface\n"+
			"  2. Create a profile and download a model for it\n"+
			"  3. Start the profile from the TUI, CLI, or web interface\n",
		config.ProjectDisplayName, paths.Config, config.ProjectName,
	)
	if write.BackupPath != "" {
		_, _ = fmt.Fprintf(output, "\nPrevious config backup: %s\n", write.BackupPath)
	}
}
