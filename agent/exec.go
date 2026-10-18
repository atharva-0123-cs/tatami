package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultTimeout bounds a single command when the caller sets none.
const DefaultTimeout = 60 * time.Second

// Result captures everything about one command execution.
type Result struct {
	ID       string
	Command  string
	Args     []string
	Dir      string
	ExitCode int
	Stdout   string
	Stderr   string
	Started  time.Time
	Finished time.Time
	Duration time.Duration
	TimedOut bool
	Canceled bool
	Denied   bool
	Err      error

	// Model usage, when an agent reports it (zero otherwise).
	TokensIn  int
	TokensOut int
	Cost      float64
}

// Success reports a clean exit (code 0, not denied/timed out).
func (r *Result) Success() bool {
	return r != nil && r.Err == nil && r.ExitCode == 0 && !r.TimedOut && !r.Canceled && !r.Denied
}

// Combined returns stdout and stderr concatenated.
func (r *Result) Combined() string {
	if r == nil {
		return ""
	}
	if r.Stderr == "" {
		return r.Stdout
	}
	if r.Stdout == "" {
		return r.Stderr
	}
	return r.Stdout + "\n" + r.Stderr
}

// FailureSummary is a compact, model-friendly description of a failure, or ""
// on success.
func (r *Result) FailureSummary() string {
	if r.Success() {
		return ""
	}
	switch {
	case r.Denied:
		return "command denied: " + r.Err.Error()
	case r.TimedOut:
		return "command timed out after " + r.Duration.Round(time.Millisecond).String() + ": " + r.Command
	case r.Canceled:
		return "command canceled: " + r.Command
	}
	var b strings.Builder
	b.WriteString("command failed (exit ")
	b.WriteString(itoa(r.ExitCode))
	b.WriteString("): ")
	b.WriteString(strings.Join(append([]string{r.Command}, r.Args...), " "))
	if errs := r.CompilerErrors(); len(errs) > 0 {
		b.WriteString("\ncompiler errors:\n")
		b.WriteString(strings.Join(limit(errs, 20), "\n"))
	}
	if fails := r.TestFailures(); len(fails) > 0 {
		b.WriteString("\ntest failures:\n")
		b.WriteString(strings.Join(limit(fails, 20), "\n"))
	}
	return b.String()
}

// goErrorRE matches Go compiler errors: file.go:line:col: message.
var goErrorRE = regexp.MustCompile(`(?m)^\S+\.(?:go|rs|c|cc|cpp|h|hpp|py|ts|js):\d+(?::\d+)?: .+$`)

// testFailRE matches common test-failure headers (Go, pytest, jest).
var testFailRE = regexp.MustCompile(`(?m)^(--- FAIL: .+|FAILED .+|_{5,} .+ _{5,}|✕ .+)$`)

// CompilerErrors extracts compiler diagnostic lines from stdout+stderr.
func (r *Result) CompilerErrors() []string { return goErrorRE.FindAllString(r.Combined(), -1) }

// TestFailures extracts test-failure lines from stdout+stderr.
func (r *Result) TestFailures() []string { return testFailRE.FindAllString(r.Combined(), -1) }

// Runner executes commands under a policy with a working directory, a
// controlled environment, a timeout and cancellation.
type Runner struct {
	Dir     string
	Timeout time.Duration
	Policy  Policy

	mu     sync.Mutex
	env    map[string]string
	cancel context.CancelFunc
	nextID atomic.Uint64
}

// NewRunner creates a runner rooted at dir. A zero timeout uses DefaultTimeout.
func NewRunner(dir string, p Policy) *Runner {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	return &Runner{Dir: dir, Timeout: DefaultTimeout, Policy: p, env: map[string]string{}}
}

// SetEnv sets (or replaces) an environment variable for child processes.
func (r *Runner) SetEnv(key, val string) {
	r.mu.Lock()
	r.env[key] = val
	r.mu.Unlock()
}

// Cancel stops the command currently running, if any.
func (r *Runner) Cancel() {
	r.mu.Lock()
	c := r.cancel
	r.mu.Unlock()
	if c != nil {
		c()
	}
}

// Run executes name with args and returns the captured result.
func (r *Runner) Run(ctx context.Context, name string, args ...string) *Result {
	res := &Result{ID: fmt.Sprintf("cmd-%d", r.nextID.Add(1)), Command: name, Args: args, Dir: r.Dir}
	if err := r.Policy.Check(name, args); err != nil {
		res.Denied = true
		res.ExitCode = -1
		res.Err = err
		return res
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		r.cancel = nil
		r.mu.Unlock()
	}()

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = r.Dir
	cmd.Env = r.childEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	res.Started = time.Now()
	err := cmd.Run()
	res.Finished = time.Now()
	res.Duration = res.Finished.Sub(res.Started)
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()

	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ExitCode = -1
		res.Err = runCtx.Err()
		// Kill the whole process group in case children survived.
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return res
	}
	if runCtx.Err() == context.Canceled {
		res.Canceled = true
		res.ExitCode = -1
		res.Err = runCtx.Err()
		return res
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
		}
		res.Err = err
		return res
	}
	res.ExitCode = 0
	return res
}

// RunShell executes a shell command line through the user's shell.
func (r *Runner) RunShell(ctx context.Context, command string) *Result {
	shell := shellName()
	flag := "-c"
	if shell == "cmd" {
		flag = "/c"
	}
	return r.Run(ctx, shell, flag, command)
}

// childEnv builds the child environment from the allowlist (or the full
// environment) overlaid with explicit overrides.
func (r *Runner) childEnv() []string {
	r.mu.Lock()
	overrides := make(map[string]string, len(r.env))
	for k, v := range r.env {
		overrides[k] = v
	}
	r.mu.Unlock()

	base := map[string]string{}
	if len(r.Policy.EnvAllow) == 0 {
		for _, e := range os.Environ() {
			if k, v, ok := strings.Cut(e, "="); ok {
				base[k] = v
			}
		}
	} else {
		for _, e := range os.Environ() {
			k, v, ok := strings.Cut(e, "=")
			if ok && contains(r.Policy.EnvAllow, k) {
				base[k] = v
			}
		}
	}
	for k, v := range overrides {
		base[k] = v
	}
	out := make([]string, 0, len(base))
	for k, v := range base {
		out = append(out, k+"="+v)
	}
	return out
}

// itoa converts an int without importing strconv into this file's import set.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// limit returns at most n elements of s.
func limit(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
