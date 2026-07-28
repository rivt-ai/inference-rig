package runtime

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"inferencerig/platform/pidfile"
)

// Neutral operational defaults for a supervised process. These are generic
// timing knobs, not engine-specific values: a caller that leaves a timing field
// unset gets a sane default; identity fields (Name/Executable/Host/Port) are
// never defaulted and stay whatever the caller supplied.
const (
	defaultStopTimeout       = 10 * time.Second
	defaultReadinessTimeout  = 30 * time.Second
	defaultReadinessInterval = 250 * time.Millisecond
	// defaultLoopbackHost is used when probing readiness of a wildcard bind.
	defaultLoopbackHost = "127.0.0.1"
)

// LaunchSpec is the neutral contract a backend hands the supervisor to describe
// how to launch and probe one process. It carries no engine terminology: the
// backend renders the executable, arguments, environment and readiness policy.
type LaunchSpec struct {
	// Name identifies the process; it is also the PID-file base name and so
	// must be a safe single path element.
	Name string
	// Executable is the absolute path or PATH-resolvable command to run.
	Executable string
	// Argv are the arguments passed to Executable.
	Argv []string
	// Env are extra environment variables layered over the parent environment.
	Env map[string]string
	// Host and Port are the readiness endpoint the process is expected to bind.
	Host string
	Port int
	// StopTimeout bounds a graceful stop before escalating to SIGKILL.
	StopTimeout time.Duration
	// ReadinessPath, when set, switches readiness probing from a raw TCP dial
	// to an HTTP GET of this path (a 2xx response means ready).
	ReadinessPath string
	// ReadinessTimeout bounds how long Start waits for the process to become
	// ready; ReadinessInterval is the delay between probes.
	ReadinessTimeout, ReadinessInterval time.Duration
	// PIDDir is the directory the PID file is written to. It is required; the
	// supervisor never guesses an engine-specific location.
	PIDDir string
	// BuildErr lets a backend defer a command-render failure so it surfaces at
	// Start (as invalid input) instead of launching a bad process.
	BuildErr error
}

func (s *LaunchSpec) applyDefaults() {
	s.StopTimeout = cmp.Or(s.StopTimeout, defaultStopTimeout)
	s.ReadinessTimeout = cmp.Or(s.ReadinessTimeout, defaultReadinessTimeout)
	s.ReadinessInterval = cmp.Or(s.ReadinessInterval, defaultReadinessInterval)
}

// Supervisor manages the full lifecycle of one process described by a
// LaunchSpec: process-group start, PID-file bookkeeping, readiness probing,
// graceful stop with SIGKILL escalation, status reporting and adopt-on-recover.
// It is engine-agnostic; backends supply the LaunchSpec.
type Supervisor struct {
	mu        sync.Mutex
	spec      LaunchSpec
	cmd       *exec.Cmd
	done      chan error
	err       error
	ready     bool
	pid, pgid int
	now       func() time.Time
	client    *http.Client
}

// NewSupervisor returns a Supervisor for spec, applying neutral timing defaults.
func NewSupervisor(spec LaunchSpec) *Supervisor {
	spec.applyDefaults()
	return &Supervisor{spec: spec, now: time.Now, client: http.DefaultClient}
}

func (s *Supervisor) Status(context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(), nil
}

func (s *Supervisor) Start(ctx context.Context) (CommandResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.start(ctx)
}

func (s *Supervisor) Stop(ctx context.Context) (CommandResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop(ctx)
}

// Recover adopts an already-running process recorded in the PID file, provided
// its executable matches the spec. A stale or mismatched PID is rejected (and
// the PID file cleared). It reports whether a process was adopted.
func (s *Supervisor) Recover(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	command, err := s.spec.command()
	if err != nil {
		return false, err
	}
	file, err := s.pidFile()
	if err != nil {
		return false, err
	}
	pid, ok, err := file.Running()
	if err != nil || !ok {
		return false, err
	}
	if !pidfile.ExecutableMatches(pid, command.Executable) {
		_ = file.Remove(pid)
		return false, nil
	}
	s.adopt(pid)
	s.ready = s.probeReady(ctx) == nil
	return true, nil
}

func (s *Supervisor) start(ctx context.Context) (CommandResult, error) {
	start := s.now()
	if s.spec.BuildErr != nil {
		err := NewError(ErrorInvalidInput, s.spec.BuildErr.Error(), s.spec.BuildErr)
		return s.commandResult("start", start, 1, "", err), err
	}
	if s.isRunning() {
		err := Errorf(ErrorRuntime, "%s is already running", s.displayName())
		return s.commandResult("start", start, 1, "", err), err
	}
	command, err := s.spec.command()
	if err != nil {
		return s.commandResult("start", start, 1, "", err), err
	}
	file, err := s.pidFile()
	if err != nil {
		return s.commandResult("start", start, 1, "", err), err
	}
	cmd := exec.Command(command.Executable, command.Argv...)
	cmd.Env = append(os.Environ(), envList(s.spec.Env)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		err = NewError(ErrorRuntime, fmt.Sprintf("start %s failed", s.displayName()), err)
		return s.commandResult("start", start, 1, "", err), err
	}
	s.started(cmd)
	recorded := make(chan struct{})
	go func(pid int) {
		err := cmd.Wait()
		<-recorded
		_ = file.Remove(pid)
		s.done <- err
	}(s.pid)
	pidErr := file.Write(s.pid)
	close(recorded)
	if pidErr != nil {
		err = NewError(ErrorRuntime, "write PID file failed", pidErr)
		return s.rollbackStart(ctx, start, err)
	}
	if err := s.waitReady(ctx); err != nil {
		s.err = err
		return s.rollbackStart(ctx, start, NewError(ErrorRuntime, err.Error(), nil))
	}
	return s.commandResult("start", start, 0, command.Display, nil), nil
}

func (s *Supervisor) stop(ctx context.Context) (CommandResult, error) {
	start := s.now()
	if !s.isRunning() {
		return s.commandResult("stop", start, 0, "", nil), nil
	}
	if err := s.stopProcess(ctx); err != nil {
		return s.commandResult("stop", start, 1, "", err), err
	}
	return s.commandResult("stop", start, 0, "", nil), nil
}

func (s *Supervisor) rollbackStart(ctx context.Context, start time.Time, err error) (CommandResult, error) {
	if cleanupErr := s.stopProcess(ctx); cleanupErr != nil {
		err = fmt.Errorf("%w; cleanup failed: %w", err, cleanupErr)
	}
	return s.commandResult("start", start, 1, "", err), err
}

func (s *Supervisor) stopProcess(ctx context.Context) error {
	pid, pgid := s.pid, s.pgid
	if pgid <= 0 {
		pgid = pid
	}
	defer func() {
		if file, err := s.pidFile(); err == nil {
			_ = file.Remove(pid)
		}
		s.pid, s.pgid, s.ready = 0, 0, false
	}()
	if !s.isRunning() {
		return nil
	}
	if err := interruptProcessGroup(pid, pgid); err != nil {
		return fmt.Errorf("%s interrupt: %v", s.displayName(), err)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, s.spec.StopTimeout)
	defer cancel()
	if s.cmd != nil {
		return s.waitStartedProcess(timeoutCtx, pgid)
	}
	if err := waitPIDExit(timeoutCtx, pid); err != nil {
		killProcessGroup(pid, pgid)
		s.err = err
		return fmt.Errorf("%s stop timed out", s.displayName())
	}
	return nil
}

func (s *Supervisor) waitReady(ctx context.Context) error {
	deadline := s.now().Add(s.spec.ReadinessTimeout)
	for {
		if !s.isRunning() {
			return fmt.Errorf("%s exited before readiness: %v", s.displayName(), s.err)
		}
		if err := s.probeReady(ctx); err == nil {
			s.ready = true
			return nil
		}
		if !s.now().Before(deadline) {
			return fmt.Errorf("%s readiness timed out", s.displayName())
		}
		timer := time.NewTimer(s.spec.ReadinessInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Supervisor) probeReady(ctx context.Context) error {
	addr := net.JoinHostPort(readinessHost(s.spec.Host), strconv.Itoa(s.spec.Port))
	if s.spec.ReadinessPath == "" {
		conn, err := net.DialTimeout("tcp", addr, s.spec.ReadinessInterval)
		if err != nil {
			return err
		}
		return conn.Close()
	}
	reqCtx, cancel := context.WithTimeout(ctx, s.spec.ReadinessInterval)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr+s.spec.ReadinessPath, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("readiness status %d", resp.StatusCode)
}

func (s *Supervisor) statusLocked() Status {
	state, pid := Stopped, 0
	if s.isRunning() {
		state, pid = Starting, s.pid
		if s.ready {
			state = Running
		}
	} else if s.pid > 0 {
		if file, err := s.pidFile(); err == nil {
			_ = file.Remove(s.pid)
		}
	}
	name := s.displayName()
	detail := name + " stopped"
	switch {
	case pid > 0:
		detail = fmt.Sprintf("%s pid=%d", name, pid)
	case s.err != nil:
		detail = fmt.Sprintf("%s stopped: %v", name, s.err)
	}
	lastErr := ""
	if s.err != nil {
		lastErr = s.err.Error()
	}
	process := ProcessStatus{Name: s.spec.Name, State: state, PID: pid, Host: s.spec.Host, Port: s.spec.Port, Ready: s.ready, LastError: lastErr}
	return Status{State: state, Detail: detail, CheckedAt: s.now().UTC(), Processes: []ProcessStatus{process}}
}

func (s *Supervisor) commandResult(action string, start time.Time, exitCode int, stdout string, err error) CommandResult {
	result := CommandResult{Action: action, ExitCode: exitCode, Stdout: stdout, DurationMS: s.now().Sub(start).Milliseconds()}
	if err != nil {
		result.Stderr = err.Error()
	}
	return result
}

func (s *Supervisor) started(cmd *exec.Cmd) {
	s.cmd, s.done, s.err, s.ready = cmd, make(chan error, 1), nil, false
	s.pid = cmd.Process.Pid
	s.pgid, _ = syscall.Getpgid(s.pid)
}

func (s *Supervisor) adopt(pid int) {
	s.cmd, s.done, s.err, s.ready = nil, nil, nil, false
	s.pid = pid
	s.pgid, _ = syscall.Getpgid(pid)
}

func (s *Supervisor) isRunning() bool {
	if s.pid <= 0 {
		return false
	}
	if s.cmd != nil {
		select {
		case err := <-s.done:
			s.err, s.ready = err, false
			return false
		default:
		}
	}
	return pidfile.Alive(s.pid)
}

func (s *Supervisor) pidFile() (pidfile.File, error) {
	name := s.spec.Name
	if s.spec.PIDDir == "" {
		return pidfile.File{}, fmt.Errorf("PID directory is not configured")
	}
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return pidfile.File{}, fmt.Errorf("invalid process name %q", name)
	}
	return pidfile.New(filepath.Join(s.spec.PIDDir, name+".pid")), nil
}

func (s *Supervisor) waitStartedProcess(ctx context.Context, pgid int) error {
	select {
	case err := <-s.done:
		s.err = err
		return nil
	case <-ctx.Done():
		killProcessGroup(s.pid, pgid)
		select {
		case err := <-s.done:
			s.err = err
		case <-time.After(time.Second):
		}
		return fmt.Errorf("%s stop timed out", s.displayName())
	}
}

// displayName is the neutral label used in status detail and error messages. It
// falls back to a generic word so messages never leak or require an engine name.
func (s *Supervisor) displayName() string {
	return cmp.Or(s.spec.Name, "process")
}

type command struct {
	Executable string
	Argv       []string
	Display    string
}

func (s *LaunchSpec) command() (command, error) {
	if s.Executable == "" {
		return command{}, Errorf(ErrorInvalidInput, "%s executable is required", cmp.Or(s.Name, "process"))
	}
	return command{Executable: s.Executable, Argv: append([]string(nil), s.Argv...), Display: strings.Join(append([]string{s.Executable}, s.Argv...), " ")}, nil
}

func interruptProcessGroup(pid, pgid int) error {
	if err := syscall.Kill(-pgid, syscall.SIGINT); err != nil {
		return syscall.Kill(pid, syscall.SIGINT)
	}
	return nil
}

func killProcessGroup(pid, pgid int) {
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func waitPIDExit(ctx context.Context, pid int) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !pidfile.Alive(pid) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

func readinessHost(host string) string {
	if host == "" || host == "0.0.0.0" || host == "::" {
		return defaultLoopbackHost
	}
	return host
}
