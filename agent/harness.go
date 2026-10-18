package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Harness ties the pieces of the built-in coding agent together: detection of
// the environment and project, a policy-checked command runner and file tools,
// and context assembly. It is deliberately model-agnostic: a caller runs
// commands, feeds the returned context into whatever model it likes, and feeds
// failures back through FailureSummary.
type Harness struct {
	Dir        string
	Env        *Environment
	Project    *Project
	Runner     *Runner
	Files      *FileTools
	Events     *EventBus
	ContextMax int

	mu          sync.Mutex
	history     []string
	lastFailure string
}

// New builds a harness for dir under the given policy.
func New(dir string, policy Policy) *Harness {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	h := &Harness{Dir: dir, Events: NewEventBus(), ContextMax: DefaultContextChars}
	h.Refresh(policy)
	return h
}

// Refresh re-runs environment and project detection and rebuilds the tools.
func (h *Harness) Refresh(policy Policy) {
	h.Env = DetectEnvironment(h.Dir)
	h.Project = DetectProject(h.Dir)
	h.Runner = NewRunner(h.Dir, policy)
	h.Files = NewFileTools(h.Dir, policy)
}

// EnvironmentJSON returns the machine-readable environment description.
func (h *Harness) EnvironmentJSON() string { return h.Env.JSON() }

// ProjectSummary renders the detected project as human-readable text.
func (h *Harness) ProjectSummary() string { return projectSummary(h.Project) }

// RunCommand runs a single program under the policy, emitting lifecycle events.
func (h *Harness) RunCommand(ctx context.Context, name string, args ...string) *Result {
	h.publish(Event{Kind: EventCommandStarted, Command: name})
	res := h.Runner.Run(ctx, name, args...)
	h.emitResult(res)
	return res
}

// RunShell runs a shell command line under the policy.
func (h *Harness) RunShell(ctx context.Context, command string) *Result {
	h.publish(Event{Kind: EventCommandStarted, Command: command})
	res := h.Runner.RunShell(ctx, command)
	h.emitResult(res)
	return res
}

// record logs a command and any failure for the context protocol.
func (h *Harness) record(res *Result) {
	h.mu.Lock()
	defer h.mu.Unlock()
	line := strings.TrimSpace(strings.Join(append([]string{res.Command}, res.Args...), " "))
	h.history = append(h.history, line)
	if len(h.history) > 100 {
		h.history = h.history[len(h.history)-100:]
	}
	if res.Success() {
		h.lastFailure = ""
	} else {
		h.lastFailure = res.FailureSummary()
	}
}

// RecentCommands returns the most recent command lines, newest last.
func (h *Harness) RecentCommands(n int) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n <= 0 || n > len(h.history) {
		n = len(h.history)
	}
	return strings.Join(h.history[len(h.history)-n:], "\n")
}

// LastFailure returns the most recent failure summary, or "".
func (h *Harness) LastFailure() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastFailure
}

// emitResult publishes the finished/failed event and classifies build/test
// failures so downstream agents can react specifically.
func (h *Harness) emitResult(res *Result) {
	h.record(res)
	switch {
	case res.Success():
		h.publish(Event{Kind: EventCommandFinished, Command: res.Command, ExitCode: 0})
		if strings.Contains(res.Command, "test") {
			h.publish(Event{Kind: EventTestPassed, Command: res.Command})
		}
	case res.Denied || res.TimedOut || res.Canceled:
		h.publish(Event{Kind: EventCommandFailed, Command: res.Command, ExitCode: res.ExitCode})
	default:
		h.publish(Event{Kind: EventCommandFailed, Command: res.Command, ExitCode: res.ExitCode})
		if len(res.CompilerErrors()) > 0 {
			h.publish(Event{Kind: EventBuildFailed, Command: res.Command, ExitCode: res.ExitCode})
		}
		if len(res.TestFailures()) > 0 {
			h.publish(Event{Kind: EventTestFailed, Command: res.Command, ExitCode: res.ExitCode})
		}
	}
}

// publish sends an event when a bus is configured.
func (h *Harness) publish(e Event) {
	if h.Events != nil {
		h.Events.Publish(e)
	}
}

// BaseContext assembles the model-independent context: environment, project
// detection and project instructions (AGENTS.md / CLAUDE.md / README). Pane and
// terminal details are layered on by the caller via AddProvider.
func (h *Harness) BaseContext() *Context {
	c := NewContext(h.ContextMax)
	c.Add("Environment", h.Env.JSON(), 0)
	c.Add("Project", projectSummary(h.Project), 1)
	if h.Project.Metadata.AgentsMD != "" {
		if text, err := h.Files.Read(h.Project.Metadata.AgentsMD); err == nil {
			c.Add("Project instructions (AGENTS.md)", FilterOutput(text, 200), 1)
		}
	}
	if h.Project.Metadata.ClaudeMD != "" {
		if text, err := h.Files.Read(h.Project.Metadata.ClaudeMD); err == nil {
			c.Add("Project instructions (CLAUDE.md)", FilterOutput(text, 200), 1)
		}
	}
	if h.Project.Metadata.Readme != "" {
		if text, err := h.Files.Read(h.Project.Metadata.Readme); err == nil {
			c.Add("README", FilterOutput(text, 100), 3)
		}
	}
	return c
}

// GitContext renders the focused repository's state for the model.
func (h *Harness) GitContext(ctx context.Context) string {
	if h.Env.Git == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "branch: %s\ncommit: %s\ndirty: %t\nahead: %d\nbehind: %d\nworktree: %t\n",
		h.Env.Git.Branch, h.Env.Git.Head, h.Env.Git.Dirty, h.Env.Git.Ahead, h.Env.Git.Behind, h.Env.Git.Worktree)
	if h.Runner != nil {
		if res := h.Runner.Run(ctx, "git", "status", "--porcelain"); res.Success() {
			b.WriteString(FilterOutput(res.Stdout, 60))
		}
	}
	return b.String()
}

// CheckGitSafety refuses Git commands that would rewrite history or discard
// work unless explicitly confirmed by the policy. It is the agent-side guard in
// front of destructive repository operations.
func (h *Harness) CheckGitSafety(name string, args []string) error {
	if name != "git" {
		return nil
	}
	return h.Runner.Policy.Check(name, args)
}

// projectSummary renders a compact description of the detected project.
func projectSummary(p *Project) string {
	if p == nil {
		return "(none)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "root: %s\ntypes: %s\n", p.Root, strings.Join(p.Types, ", "))
	if p.Metadata.GitRepo {
		b.WriteString("git repository: yes\n")
	}
	if len(p.BuildConfig) > 0 {
		fmt.Fprintf(&b, "build config: %s\n", strings.Join(p.BuildConfig, ", "))
	}
	if len(p.TestConfig) > 0 {
		fmt.Fprintf(&b, "test config: %s\n", strings.Join(p.TestConfig, ", "))
	}
	var meta []string
	if p.Metadata.Readme != "" {
		meta = append(meta, "README")
	}
	if p.Metadata.AgentsMD != "" {
		meta = append(meta, "AGENTS.md")
	}
	if p.Metadata.ClaudeMD != "" {
		meta = append(meta, "CLAUDE.md")
	}
	if p.Metadata.Cursor {
		meta = append(meta, ".cursor/")
	}
	if p.Metadata.GitHub {
		meta = append(meta, ".github/")
	}
	if len(meta) > 0 {
		fmt.Fprintf(&b, "metadata: %s\n", strings.Join(meta, ", "))
	}
	return b.String()
}
