package falkor

import (
	"strings"
	"testing"
)

func TestParseQueryResult(t *testing.T) {
	raw := []interface{}{
		[]interface{}{
			[]interface{}{"Column", "name"},
			[]interface{}{"Column", "path"},
		},
		[]interface{}{
			[]interface{}{"Widget", "pkg/widget.py"},
			[]interface{}{"helper", "pkg/widget.py"},
		},
		[]interface{}{},
	}
	qr := ParseQueryResult(raw)
	if len(qr.Columns) != 2 || qr.Columns[0] != "name" || qr.Columns[1] != "path" {
		t.Fatalf("columns = %v", qr.Columns)
	}
	if len(qr.Rows) != 2 {
		t.Fatalf("rows = %v", qr.Rows)
	}
	if got := qr.Summarize(); got != "2 cols, 2 rows" {
		t.Errorf("Summarize = %q", got)
	}
	out := qr.Format()
	if !strings.Contains(out, "name | path") || !strings.Contains(out, "Widget | pkg/widget.py") {
		t.Errorf("unexpected format output:\n%s", out)
	}
}

func TestParseQueryResultFallback(t *testing.T) {
	qr := ParseQueryResult("PONG")
	if qr.Summarize() != "PONG" {
		t.Errorf("Summarize fallback = %q", qr.Summarize())
	}
	if got := FormatResult("PONG"); got != "PONG" {
		t.Errorf("FormatResult fallback = %q", got)
	}
}

func TestResolveBuiltin(t *testing.T) {
	if _, ok := ResolveBuiltin(":files"); !ok {
		t.Errorf(":files should resolve")
	}
	q, ok := ResolveBuiltin(":callers helper")
	if !ok || !strings.Contains(q, `fn.name = "helper"`) {
		t.Errorf(":callers helper = %q, ok=%v", q, ok)
	}
	if _, ok := ResolveBuiltin(":callers"); !ok {
		t.Errorf(":callers without arg should fall back to the ranked query")
	}
	if _, ok := ResolveBuiltin(":deps"); ok {
		t.Errorf(":deps requires an argument")
	}
	if _, ok := ResolveBuiltin(":nope"); ok {
		t.Errorf(":nope should not resolve")
	}
	raw, ok := ResolveBuiltin("MATCH (n) RETURN n")
	if !ok || raw != "MATCH (n) RETURN n" {
		t.Errorf("raw cypher = %q, ok=%v", raw, ok)
	}
	if _, ok := ResolveBuiltin("   "); ok {
		t.Errorf("blank input should not resolve")
	}
}
