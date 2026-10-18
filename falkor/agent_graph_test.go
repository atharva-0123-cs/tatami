package falkor

import (
	"strings"
	"testing"
)

func TestAgentMemoryCypher(t *testing.T) {
	m := NewAgentMemory()
	m.AddAgent("a1", "coder")
	m.AddFinding("a1", "f1", "nil deref in parser")
	m.AddBug("b1", "nil deref in parser", "high", "open")
	m.ReportBug("f1", "b1")
	m.AddBugFile("b1", "./parser.go")
	m.AddTask("t1", "fix parser", "todo")
	m.AddTask("t2", "add test", "todo")
	m.AssignTask("a1", "t1")
	m.AddTaskDependency("t1", "t2")

	stmts := strings.Join(m.Cypher(), "\n")
	for _, want := range []string{"Agent", "Finding", "Bug", "Task", "REPORTS", "AFFECTS", "DEPENDS_ON", "BLOCKS", "ASSIGNED_TO", "FOUND"} {
		if !strings.Contains(stmts, want) {
			t.Errorf("expected generated Cypher to contain %q\n%s", want, stmts)
		}
	}
}

func TestStableSlug(t *testing.T) {
	if got := stableSlug("Nil deref in Parser!"); got != "nil-deref-in-parser" {
		t.Errorf("stableSlug = %q", got)
	}
	if got := stableSlug(""); got != "item" {
		t.Errorf("stableSlug(empty) = %q", got)
	}
}
