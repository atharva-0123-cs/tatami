package falkor

import "testing"

func TestGraphAddNodeMergesProps(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "x", Label: LabelFile, Props: map[string]string{"a": "1"}})
	g.AddNode(Node{ID: "x", Label: LabelFile, Props: map[string]string{"b": "2"}})
	if g.NodeCount() != 1 {
		t.Fatalf("expected 1 node, got %d", g.NodeCount())
	}
	n := g.Nodes()[0]
	if n.Props["a"] != "1" || n.Props["b"] != "2" {
		t.Errorf("props were not merged: %v", n.Props)
	}
}

func TestGraphAddEdgeDeduplicates(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "a", Label: LabelFile})
	g.AddNode(Node{ID: "b", Label: LabelImport})
	g.AddEdge("a", "b", RelImports)
	g.AddEdge("a", "b", RelImports)
	if g.EdgeCount() != 1 {
		t.Fatalf("expected 1 edge, got %d", g.EdgeCount())
	}
}

func TestGraphCypherEscaping(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: `weird"id`, Label: LabelFile, Props: map[string]string{"path": `a\b"c`}})
	stmts := g.Cypher()
	if len(stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(stmts))
	}
	got := stmts[0]
	if !contains(got, `id: "weird\"id"`) {
		t.Errorf("id not escaped: %s", got)
	}
	if !contains(got, `path = "a\\b\"c"`) {
		t.Errorf("value not escaped: %s", got)
	}
}

func TestGraphResolvePrefersSameFile(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "fileA", Label: LabelFile})
	g.AddNode(Node{ID: "fileB", Label: LabelFile})
	g.AddNode(Node{ID: "a1", Label: LabelFunction, Props: map[string]string{"name": "dup", "file": "fileA"}})
	g.AddNode(Node{ID: "b1", Label: LabelFunction, Props: map[string]string{"name": "dup", "file": "fileB"}})
	g.AddNode(Node{ID: "caller", Label: LabelFunction, Props: map[string]string{"name": "caller", "file": "fileB"}})
	g.AddCall("caller", "fileB", "dup")

	if n := g.Resolve(); n != 1 {
		t.Fatalf("expected 1 resolved call, got %d", n)
	}
	if !hasEdge(g, "caller", "b1", RelCalls) {
		t.Errorf("expected same-file target b1")
	}
	if hasEdge(g, "caller", "a1", RelCalls) {
		t.Errorf("should not link to other-file target")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
