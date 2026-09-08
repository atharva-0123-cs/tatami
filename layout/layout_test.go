package layout

import (
	"testing"

	"github.com/atharva-0123-cs/tview-demo/pane"
)

// newWMWithPanes builds a Stacked (SplitHorizontal) tree of n zero-value panes
// and returns the manager plus the panes in traversal order.
//
// The panes are zero-value pane.Pane structs (no PTY/emulator) so the tests
// exercise only the tree/bookkeeping logic; the manager's focus ends on the
// last inserted pane.
func newWMWithPanes(t *testing.T, n int) (*WindowManager, []*pane.Pane) {
	t.Helper()
	wm := NewWindowManager()
	first := &pane.Pane{}
	wm.root = &LayoutNode{Leaf: true, Pane: first}
	wm.focused = first
	panes := []*pane.Pane{first}
	for i := 1; i < n; i++ {
		next := &pane.Pane{}
		wm.splitCurrent(next, SplitHorizontal)
		panes = append(panes, next)
	}
	return wm, panes
}

// TestSplitCurrentBuildsTree verifies that repeated horizontal splits build a
// container root with the right pane count, that the newest pane takes focus,
// and that PaneIndex matches traversal order.
func TestSplitCurrentBuildsTree(t *testing.T) {
	wm, panes := newWMWithPanes(t, 3)
	if got := wm.PaneCount(); got != 3 {
		t.Fatalf("PaneCount = %d, want 3", got)
	}
	if wm.root.Leaf {
		t.Fatal("root should be a container after splits")
	}
	if wm.FocusedPane() != panes[2] {
		t.Fatal("new pane should be focused after split")
	}
	if got := wm.PaneIndex(panes[1]); got != 2 {
		t.Fatalf("PaneIndex(panes[1]) = %d, want 2", got)
	}
}

// TestCloseCurrentRefocusesSibling verifies that closing a non-edge pane
// removes it and refocuses its next sibling, without quitting the app.
func TestCloseCurrentRefocusesSibling(t *testing.T) {
	wm, panes := newWMWithPanes(t, 3)
	wm.FocusPane(panes[0])
	if wm.CloseCurrentPane() {
		t.Fatal("closing a middle pane should not quit")
	}
	if wm.FocusedPane() != panes[1] {
		t.Fatalf("focused = %p, want panes[1] (%p)", wm.FocusedPane(), panes[1])
	}
	if got := wm.PaneCount(); got != 2 {
		t.Fatalf("PaneCount = %d, want 2", got)
	}
}

// TestCloseLastPaneQuits verifies that closing the only remaining pane clears
// the tree and reports quit=true.
func TestCloseLastPaneQuits(t *testing.T) {
	wm, _ := newWMWithPanes(t, 1)
	if !wm.CloseCurrentPane() {
		t.Fatal("closing the last pane should signal quit")
	}
	if wm.root != nil || wm.FocusedPane() != nil {
		t.Fatal("tree and focus should be cleared after closing the last pane")
	}
}

// TestCloseDeadPanes verifies auto-close: dead panes are removed and focus
// moves to the next sibling, and closing the final dead pane signals quit.
func TestCloseDeadPanes(t *testing.T) {
	wm, panes := newWMWithPanes(t, 3)

	panes[0].MarkDead("exited")
	if wm.CloseDeadPanes() {
		t.Fatal("closing one dead pane with others alive should not quit")
	}
	if wm.FocusedPane() != panes[1] {
		t.Fatal("focus should move to the next sibling after removing a dead pane")
	}
	if got := wm.PaneCount(); got != 2 {
		t.Fatalf("PaneCount = %d, want 2", got)
	}
	if panes[0].IsDead() && wm.findLeaf(panes[0]) != nil {
		t.Fatal("dead pane should be removed from the tree")
	}

	panes[1].MarkDead("exited")
	panes[2].MarkDead("exited")
	if !wm.CloseDeadPanes() {
		t.Fatal("closing the final dead pane should signal quit")
	}
	if wm.root != nil {
		t.Fatal("tree should be empty after the last pane closes")
	}
}

// TestCloseDeadPanesNoDead verifies CloseDeadPanes is a no-op (and never quits)
// when every pane is alive.
func TestCloseDeadPanesNoDead(t *testing.T) {
	wm, _ := newWMWithPanes(t, 2)
	if wm.CloseDeadPanes() {
		t.Fatal("CloseDeadPanes should not quit when no pane is dead")
	}
}

// TestSwapNext verifies SwapNext swaps the focused pane with its next sibling
// (the first pane of the tree changes accordingly).
func TestSwapNext(t *testing.T) {
	wm, panes := newWMWithPanes(t, 3)
	wm.FocusPane(panes[0])
	wm.SwapNext()
	if wm.firstPane(wm.root) != panes[1] {
		t.Fatal("first pane should be panes[1] after swapping forward")
	}
}

// TestMoveFocusDirectional verifies MoveFocus steps focus to the next/previous
// sibling in the given split direction and clamps at the edges.
func TestMoveFocusDirectional(t *testing.T) {
	wm, panes := newWMWithPanes(t, 3)
	wm.FocusPane(panes[0])
	wm.MoveFocus(SplitHorizontal, 1)
	if wm.FocusedPane() != panes[1] {
		t.Fatal("MoveFocus(+1) should move to the next stacked sibling")
	}
	wm.MoveFocus(SplitHorizontal, -1)
	if wm.FocusedPane() != panes[0] {
		t.Fatal("MoveFocus(-1) should move back to the previous sibling")
	}
}

// TestSeamSpansSharedEdge verifies that every span after the first starts one
// cell before the previous span ends, so adjacent panes share a single border
// row/column instead of two parallel borders.
func TestSeamSpansSharedEdge(t *testing.T) {
	sp := seamSpans(20, []int{1, 1})
	if sp[0] != [2]int{0, 10} || sp[1] != [2]int{9, 20} {
		t.Fatalf("spans = %v, want {[0 10] [9 20]}", sp)
	}
	// Three children: each internal boundary overlaps by exactly one cell.
	sp = seamSpans(30, []int{1, 1, 1})
	want := [][2]int{{0, 10}, {9, 20}, {19, 30}}
	for i := range want {
		if sp[i] != want[i] {
			t.Fatalf("spans[%d] = %v, want %v", i, sp[i], want[i])
		}
		// The shared cell must lie inside *both* panes' spans.
		if i > 0 && sp[i][0] != sp[i-1][1]-1 {
			t.Fatalf("spans %v and %v do not share an edge", sp[i-1], sp[i])
		}
	}
}

// TestSeamSpansEdgeCases covers the remainder and degenerate cases: the last
// child absorbs rounding, zero-size children produce no span, and a lone child
// spans the whole axis untouched.
func TestSeamSpansEdgeCases(t *testing.T) {
	if sp := seamSpans(21, []int{1, 1}); sp[1] != [2]int{9, 21} {
		t.Fatalf("remainder spans = %v, want {[0 10] [9 21]}", sp)
	}
	if sp := seamSpans(0, []int{1, 4, 5}); sp[1] != [2]int{0, 0} {
		t.Fatalf("zero-avail spans = %v, want zero spans", sp)
	}
	if sp := seamSpans(40, []int{2}); sp[0] != [2]int{0, 40} {
		t.Fatalf("single span = %v, want {[0 40]}", sp)
	}
	// No sizes at all: empty result, no panic.
	if sp := seamSpans(10, nil); len(sp) != 0 {
		t.Fatalf("nil sizes = %v, want empty", sp)
	}
}
