package app

import "testing"

func TestCommandWordPrefix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"spl", "spl"},
		{"new-window", "new-window"},
		{"select-pane pane-", "pane-"},
		{"select-pane pane-1", "pane-1"},
		{"select-pane ", ""},
		{"rename-pane x name", "name"},
	}
	for _, c := range cases {
		if got := commandWordPrefix(c.in); got != c.want {
			t.Errorf("commandWordPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMatchingCICaseInsensitiveUnique(t *testing.T) {
	got := matchingCI([]string{"Split", "vsplit", "split"}, "sp")
	if len(got) != 2 || got[0] != "Split" || got[1] != "split" {
		t.Fatalf("matchingCI = %v, want [Split split]", got)
	}
}

func TestCommandCompletionsFirstTokenIncludesAliases(t *testing.T) {
	a := &App{cmdBuf: "sp"}
	got := a.commandCompletions()
	if !contains(got, "split") || !contains(got, "sp") {
		t.Fatalf("completions for %q = %v, want split and sp", a.cmdBuf, got)
	}
}

func TestCommandCompletionsArgsOnlyForPaneCommands(t *testing.T) {
	a := &App{cmdBuf: "help "}
	if got := a.commandCompletions(); len(got) != 0 {
		t.Fatalf("help arg completions = %v, want none", got)
	}
}

func TestCompleteCommandCycles(t *testing.T) {
	a := &App{cmdBuf: "ne"}
	a.completeCommand(1)
	if a.cmdBuf != "new-window" || !a.cmdCompActive {
		t.Fatalf("first completion = %q active=%v, want new-window", a.cmdBuf, a.cmdCompActive)
	}
	a.completeCommand(1)
	if a.cmdBuf != "next-window" {
		t.Fatalf("second completion = %q, want next-window", a.cmdBuf)
	}
	a.completeCommand(-1)
	if a.cmdBuf != "new-window" {
		t.Fatalf("backtab completion = %q, want new-window", a.cmdBuf)
	}
}

func TestCompleteCommandResetOnEdit(t *testing.T) {
	a := &App{cmdBuf: "ne"}
	a.completeCommand(1)
	if !a.cmdCompActive {
		t.Fatal("expected completion active")
	}
	a.resetCommandCompletion()
	if a.cmdCompActive || a.cmdBuf != "new-window" {
		t.Fatalf("reset left active=%v buf=%q", a.cmdCompActive, a.cmdBuf)
	}
}

func TestCommandCandidateLabel(t *testing.T) {
	if got := commandCandidateLabel("split"); got == "split" {
		t.Fatalf("split label missing help: %q", got)
	}
	if got := commandCandidateLabel("sp"); got != "sp  —  alias for split" {
		t.Fatalf("alias label = %q", got)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
