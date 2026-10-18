package falkor

import (
	"testing"

	"github.com/atharva-0123-cs/tatami/layout"
)

func TestWorkspaceGraphGeneration(t *testing.T) {
	spec := &layout.SessionSpec{
		Name:   "dev",
		Active: 0,
		Windows: []layout.WindowSpec{{
			Name:         "1",
			FocusedIndex: 0,
			Root: &layout.NodeSpec{
				Leaf:  false,
				Dir:   1,
				Sizes: []int{1, 1},
				Children: []*layout.NodeSpec{
					{Leaf: true, Cwd: "/home/user/a"},
					{Leaf: true, Cwd: "/home/user/b"},
				},
			},
		}},
	}

	wg := NewWorkspaceGraph()
	wg.AddSession(spec)
	wg.AddPaneProcess("window://1/root/c0", 1234, 1000, "bash")

	stmts := wg.Cypher()
	if len(stmts) == 0 {
		t.Fatalf("expected generated cypher statements, got 0")
	}

	// Each pane's working directory should be modeled as a Directory node.
	g := wg.graph
	if _, ok := g.nodes["dir:///home/user/a"]; !ok {
		t.Errorf("expected Directory node for pane cwd")
	}
	if _, ok := g.nodes["proc://1234"]; !ok {
		t.Errorf("expected Process node for pane process")
	}
}
