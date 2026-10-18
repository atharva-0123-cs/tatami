package app

import "testing"

// TestCurrentCompletionsBuiltins verifies that a ":prefix" first token offers
// the matching ":name" builtins.
func TestCurrentCompletionsBuiltins(t *testing.T) {
	a := &App{graphBuf: ":fi"}
	got := a.currentCompletions()
	want := map[string]bool{":files": false, ":fixes": false}
	for _, c := range got {
		if _, ok := want[c]; ok {
			want[c] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("completions %v missing %s", got, name)
		}
	}
}

// TestCurrentCompletionsKeywords verifies case-insensitive Cypher keyword
// completion in a non-first token position.
func TestCurrentCompletionsKeywords(t *testing.T) {
	a := &App{graphBuf: "MATCH (f) RET"}
	found := false
	for _, c := range a.currentCompletions() {
		if c == "RETURN" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected RETURN among completions of RET")
	}
}

// TestCompleteGraphCycles verifies TAB captures a candidate list and cycles it
// deterministically, wrapping around.
func TestCompleteGraphCycles(t *testing.T) {
	a := &App{graphBuf: ":fi"}
	a.completeGraph(1)
	first := a.graphBuf
	if first != ":files" {
		t.Fatalf("first TAB = %q, want :files", first)
	}
	n := len(a.graphCompItems)
	for i := 0; i < n-1; i++ {
		a.completeGraph(1)
		if a.graphBuf == first {
			t.Fatalf("cycling wrapped early at %d candidates", n)
		}
	}
	a.completeGraph(1)
	if a.graphBuf != first {
		t.Fatalf("TAB should wrap back to %q, got %q", first, a.graphBuf)
	}
	a.completeGraph(-1)
	if a.graphBuf == first {
		t.Fatalf("backtab should step backwards, stayed at %q", first)
	}
}

// TestCompleteGraphResetOnEdit verifies an edit abandons the cycle so the next
// TAB recomputes candidates.
func TestCompleteGraphResetOnEdit(t *testing.T) {
	a := &App{graphBuf: ":fi"}
	a.completeGraph(1)
	if !a.graphCompActive {
		t.Fatal("completion should be active after TAB")
	}
	a.resetGraphCompletion()
	if a.graphCompActive {
		t.Fatal("completion should be inactive after reset")
	}
}

// TestCompleteGraphPreservesHead verifies only the trailing token is replaced.
func TestCompleteGraphPreservesHead(t *testing.T) {
	a := &App{graphBuf: "MATCH (n) RET"}
	a.completeGraph(1)
	if a.graphBuf != "MATCH (n) RETURN" {
		t.Fatalf("got %q, want %q", a.graphBuf, "MATCH (n) RETURN")
	}
}
