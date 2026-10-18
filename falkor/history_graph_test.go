package falkor

import (
	"strings"
	"testing"
)

func TestCommandHistoryCypher(t *testing.T) {
	h := NewCommandHistory()
	id := h.Add("run-1", "go test ./...", 1, "FAIL: TestFoo", "/proj", 4242, []string{"./parser.go"})
	if id != "cmd://run-1" {
		t.Fatalf("id = %q", id)
	}
	stmts := strings.Join(h.Cypher(), "\n")
	for _, want := range []string{"Command", "status", "failed", "AFFECTED", "EXECUTED", "PRODUCES", "Output"} {
		if !strings.Contains(stmts, want) {
			t.Errorf("expected history Cypher to contain %q\n%s", want, stmts)
		}
	}
}

func TestCommandHistorySuccessHasNoOutput(t *testing.T) {
	h := NewCommandHistory()
	h.Add("run-2", "go build", 0, "", "/proj", 0, nil)
	stmts := strings.Join(h.Cypher(), "\n")
	if strings.Contains(stmts, "PRODUCES") {
		t.Errorf("successful command should not produce an Output node:\n%s", stmts)
	}
	if !strings.Contains(stmts, `"ok"`) {
		t.Errorf("expected status ok:\n%s", stmts)
	}
}
