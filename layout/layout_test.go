package layout

import (
	"testing"

	"github.com/atharva-0123-cs/tatami/pane"
	"github.com/atharva-0123-cs/tatami/window"
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
	wm.root = &window.Node{Leaf: true, Pane: first}
	wm.focused = first
	panes := []*pane.Pane{first}
	for i := 1; i < n; i++ {
		next := &pane.Pane{}
		wm.splitCurrent(next, window.SplitHorizontal)
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
	wm.MoveFocus(window.SplitHorizontal, 1)
	if wm.FocusedPane() != panes[1] {
		t.Fatal("MoveFocus(+1) should move to the next stacked sibling")
	}
	wm.MoveFocus(window.SplitHorizontal, -1)
	if wm.FocusedPane() != panes[0] {
		t.Fatal("MoveFocus(-1) should move back to the previous sibling")
	}
}

// TestSeamSpansSharedEdge verifies that every span after the first starts one
// cell before the previous span ends. That overlap is what keeps the gutter
// between two adjacent panes at PaneGap cells: after both panes inset by
// PaneGap, they leave exactly one shared cell blank instead of two.
func TestSeamSpansSharedEdge(t *testing.T) {
	wm := NewWindowManager() // default gap=1 -> inset=1, overlap=1
	sp := wm.seamSpans(20, []int{1, 1})
	if sp[0] != [2]int{0, 10} || sp[1] != [2]int{9, 20} {
		t.Fatalf("spans = %v, want {[0 10] [9 20]}", sp)
	}
	// Three children: each internal boundary overlaps by exactly one cell.
	sp = wm.seamSpans(30, []int{1, 1, 1})
	want := [][2]int{{0, 10}, {9, 20}, {19, 30}}
	for i := range want {
		if sp[i] != want[i] {
			t.Fatalf("spans[%d] = %v, want %v", i, sp[i], want[i])
		}
		if i > 0 && sp[i][0] != sp[i-1][1]-1 {
			t.Fatalf("spans %v and %v do not share an edge", sp[i-1], sp[i])
		}
	}
	// The pane gap must be positive so the inset actually separates panes.
	if wm.PaneGap() < 1 {
		t.Fatalf("PaneGap = %d, want at least 1 cell of separation", wm.PaneGap())
	}
}

// TestSeamSpansEdgeCases covers the remainder and degenerate cases: the last
// child absorbs rounding, zero-size children produce no span, and a lone child
// spans the whole axis untouched.
func TestSeamSpansEdgeCases(t *testing.T) {
	wm := NewWindowManager()
	if sp := wm.seamSpans(21, []int{1, 1}); sp[1] != [2]int{9, 21} {
		t.Fatalf("remainder spans = %v, want {[0 10] [9 21]}", sp)
	}
	if sp := wm.seamSpans(0, []int{1, 4, 5}); sp[1] != [2]int{0, 0} {
		t.Fatalf("zero-avail spans = %v, want zero spans", sp)
	}
	if sp := wm.seamSpans(40, []int{2}); sp[0] != [2]int{0, 40} {
		t.Fatalf("single span = %v, want {[0 40]}", sp)
	}
	// No sizes at all: empty result, no panic.
	if sp := wm.seamSpans(10, nil); len(sp) != 0 {
		t.Fatalf("nil sizes = %v, want empty", sp)
	}
}

// TestGapGeometry verifies the gap derives a valid inset/overlap pair and that
// SetPaneGap is reflected in seam span overlaps plus the default PaneGap().
func TestGapGeometry(t *testing.T) {
	want := map[int][2]int{ // gap -> {inset, overlap}
		0: {1, 2},
		1: {1, 1},
		2: {1, 0},
		3: {2, 1},
		4: {2, 0},
		6: {3, 0},
	}
	for gap, pair := range want {
		g, inset, overlap := gapGeometry(gap)
		if g != gap {
			t.Fatalf("gapGeometry(%d) returned gap %d", gap, g)
		}
		if inset != pair[0] || overlap != pair[1] {
			t.Fatalf("gapGeometry(%d) = inset %d, overlap %d; want %v/%v",
				gap, inset, overlap, pair[0], pair[1])
		}
		if 2*inset-overlap != gap {
			t.Fatalf("gapGeometry(%d): 2*inset-overlap = %d, want gutter %d",
				gap, 2*inset-overlap, gap)
		}
	}
	// Negative gaps clamp to 0 (touching panes).
	if g, i, o := gapGeometry(-5); g != 0 || 2*i-o != 0 {
		t.Fatalf("negative gap should behave like gap 0, got %d/%d/%d", g, i, o)
	}

	wm := NewWindowManager()
	if wm.PaneGap() != 1 || wm.inset != 1 || wm.overlap != 1 {
		t.Fatalf("default wm gap = %d (inset %d, overlap %d), want 1/1/1",
			wm.PaneGap(), wm.inset, wm.overlap)
	}
	if wm.outer != 0 {
		t.Fatalf("default outer margin = %d, want 0 (flush)", wm.outer)
	}
	wm.SetPaneGap(3)
	if wm.PaneGap() != 3 || wm.overlap != 1 {
		t.Fatalf("after SetPaneGap(3): gap=%d overlap=%d, want 3/1", wm.PaneGap(), wm.overlap)
	}
	if wm.outer != 1 {
		t.Fatalf("SetPaneGap(3) outer margin = %d, want 1 (inset-1)", wm.outer)
	}
	// Edge sides get the outer margin; internal sides get the inset.
	l, tp, r, b := wm.edgeInsets(0, 0, 40, 10, 80, 21)
	if l != 1 || tp != 1 {
		t.Fatalf("region-edge insets = left %d top %d, want 1/1", l, tp)
	}
	if r != wm.inset || b != wm.inset {
		t.Fatalf("internal insets = right %d bottom %d, want %d", r, b, wm.inset)
	}
	l, tp, r, b = wm.edgeInsets(40, 5, 80, 21, 80, 21)
	if r != 1 || b != 1 {
		t.Fatalf("bottom-right region insets = right %d bottom %d, want 1/1", r, b)
	}
	if l != wm.inset || tp != wm.inset {
		t.Fatalf("top-left internal insets = left %d top %d, want %d", l, tp, wm.inset)
	}
	// A wider gap overlaps less: two siblings in 20 columns split with overlap.
	wm.SetPaneGap(2)
	if sp := wm.seamSpans(20, []int{1, 1}); sp[1] != [2]int{10, 20} {
		t.Fatalf("gap=2 spans = %v, want contiguous {[0 10] [10 20]}", sp)
	}
	wm.SetPaneGap(0)
	if sp := wm.seamSpans(20, []int{1, 1}); sp[1] != [2]int{8, 20} {
		t.Fatalf("gap=0 spans = %v, want {[0 10] [8 20]}", sp)
	}
}

// TestFocusAt verifies mouse-style focus: clicking inside a pane's inset
// rectangle focuses it, clicking on borders/gaps or outside the tree does not.
func TestFocusAt(t *testing.T) {
	wm, panes := newWMWithPanes(t, 3)
	wm.FocusPane(panes[2])

	// 3 stacked panes in an 80x(21+1) terminal. Each pane gets 7 rows of the
	// pane region; internal edges inset by 1 cell, outer edges are flush, so
	// the panes occupy rows [0,6), [7,13), [14,21).
	screenW, screenH := 80, 22
	if !wm.FocusAt(5, 2, screenW, screenH) {
		t.Fatal("click in pane 1 should focus a pane")
	}
	if wm.FocusedPane() != panes[0] {
		t.Fatal("click in top region should focus pane 1")
	}
	if !wm.FocusAt(5, 10, screenW, screenH) {
		t.Fatal("click in pane 2 should focus a pane")
	}
	if wm.FocusedPane() != panes[1] {
		t.Fatal("click in middle region should focus pane 2")
	}
	if !wm.FocusAt(5, 18, screenW, screenH) {
		t.Fatal("click in pane 3 should focus a pane")
	}
	if wm.FocusedPane() != panes[2] {
		t.Fatal("click in bottom region should focus pane 3")
	}
	// Status bar area is not part of any pane.
	if wm.FocusAt(5, 21, screenW, screenH) {
		t.Fatal("click on status bar should not focus a pane")
	}
	// Click in the gap between panes (no hit).
	if wm.FocusAt(5, 6, screenW, screenH) {
		t.Fatal("click in the pane gap should not focus a pane")
	}
	// With the default gap the outer margin is zero, so the leftmost column
	// is the pane's own border and belongs to the pane.
	if !wm.FocusAt(0, 2, screenW, screenH) {
		t.Fatal("click on the flush left edge should focus a pane")
	}
	if wm.FocusedPane() != panes[0] {
		t.Fatal("click on the flush left edge should focus pane 1")
	}
}
