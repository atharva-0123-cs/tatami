package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDetectEnvironmentJSON(t *testing.T) {
	env := DetectEnvironment(t.TempDir())
	if env.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", env.OS, runtime.GOOS)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(env.JSON()), &decoded); err != nil {
		t.Fatalf("environment JSON invalid: %v", err)
	}
	for _, key := range []string{"os", "arch", "shell", "cwd"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("environment JSON missing %q", key)
		}
	}
}

func TestDetectProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := DetectProject(dir)
	if len(p.Types) != 1 || p.Types[0] != TypeGo {
		t.Errorf("types = %v, want [go]", p.Types)
	}
	if p.Metadata.AgentsMD != "AGENTS.md" {
		t.Errorf("AgentsMD = %q", p.Metadata.AgentsMD)
	}
	if !p.Metadata.GitHub {
		t.Errorf("expected .github detected")
	}
	if len(p.BuildConfig) == 0 {
		t.Errorf("expected build config detected")
	}
}

func TestRunnerSuccessAndDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell tools")
	}
	r := NewRunner(t.TempDir(), Policy{})
	res := r.Run(context.Background(), "echo", "hello")
	if !res.Success() {
		t.Fatalf("echo failed: %+v", res)
	}
	if strings.TrimSpace(res.Stdout) != "hello" {
		t.Errorf("stdout = %q", res.Stdout)
	}

	deny := NewRunner(t.TempDir(), Policy{Deny: []string{"echo"}})
	dres := deny.Run(context.Background(), "echo", "hi")
	if !dres.Denied || dres.Success() {
		t.Errorf("expected echo to be denied: %+v", dres)
	}
}

func TestRunnerTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell tools")
	}
	r := NewRunner(t.TempDir(), Policy{})
	r.Timeout = 200 * time.Millisecond
	res := r.Run(context.Background(), "sleep", "5")
	if !res.TimedOut {
		t.Fatalf("expected timeout, got %+v", res)
	}
}

func TestRunnerFailureSummaryParsesCompilerErrors(t *testing.T) {
	res := &Result{
		Command:  "go",
		Args:     []string{"build"},
		ExitCode: 1,
		Stderr:   "main.go:12:5: undefined: foo\nsome other output",
	}
	summary := res.FailureSummary()
	if !strings.Contains(summary, "main.go:12:5") {
		t.Errorf("expected compiler error in summary:\n%s", summary)
	}
}

func TestPolicyReadOnly(t *testing.T) {
	p := Policy{ReadOnly: true}
	if err := p.Check("rm", []string{"-rf", "/"}); err == nil {
		t.Errorf("expected rm to be blocked under read-only policy")
	}
	if err := p.Check("ls", nil); err != nil {
		t.Errorf("ls should be allowed: %v", err)
	}
}

func TestFilterOutputTruncatesMiddle(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	out := FilterOutput(strings.Join(lines, "\n"), 10)
	if !strings.Contains(out, "omitted") {
		t.Errorf("expected omission marker:\n%s", out)
	}
}

func TestContextBudget(t *testing.T) {
	c := NewContext(80)
	c.Add("low", strings.Repeat("x", 200), 5)
	c.Add("high", "important", 0)
	out := c.Render()
	if !strings.Contains(out, "important") {
		t.Errorf("high-priority section missing:\n%s", out)
	}
	if len(out) > 200 {
		t.Errorf("context too large: %d bytes", len(out))
	}
}

func TestFileToolsEscape(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTools(dir, Policy{})
	if _, err := ft.Read("../secret"); err == nil {
		t.Errorf("expected escape to be rejected")
	}
	if err := ft.Write("sub/a.txt", "data"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ft.Read("sub/a.txt")
	if err != nil || got != "data" {
		t.Fatalf("read = %q, %v", got, err)
	}

	ro := NewFileTools(dir, Policy{ReadOnly: true})
	if err := ro.Write("b.txt", "x"); err == nil {
		t.Errorf("expected read-only write to fail")
	}
}

func TestEventBus(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(1)
	bus.Publish(Event{Kind: EventPaneCreated, PaneID: "pane-1"})
	select {
	case e := <-ch:
		if e.Kind != EventPaneCreated || e.PaneID != "pane-1" {
			t.Errorf("unexpected event: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
	}
	bus.Unsubscribe(ch)
}
