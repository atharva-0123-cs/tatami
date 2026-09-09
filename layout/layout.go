// Package layout implements the WindowManager: a tree of LayoutNodes (each
// leaf holds one Pane, each container splits its children in one direction)
// plus focus, zoom, split, close, swap, rotate and resize operations. Every
// render pass walks the tree once to assign each pane a screen rectangle,
// draws each pane into the tcell screen, and paints the status bar.
//
// The implementation is split across files by concern: draw.go renders the
// layout and status bar, splits.go creates panes, close.go removes them,
// focus.go moves focus, transform.go zooms/swaps/rotates, and resize.go
// adjusts proportions. This file holds the tree types and primitives.
package layout

import (
	"fmt"

	"github.com/atharva-0123-cs/tatami/pane"
)

// SplitDir represents the direction of a split.
type SplitDir int

const (
	SplitHorizontal SplitDir = iota // children laid out top-to-bottom
	SplitVertical                   // children laid out left-to-right
)

// StatusBarHeight is the number of terminal rows reserved for the status line.
const StatusBarHeight = 1

// LayoutNode is a node in the layout tree. A leaf holds a single Pane;
// a container holds children arranged in one direction.
//
// Sibling rectangles within one container partition the container's rectangle
// proportionally to their sizes (seamSpans). Each leaf insets its assigned
// rectangle by the window's gap inset on every side when drawing, so adjacent
// panes keep a thin visible gutter instead of touching borders. Leaf nodes have
// a nil children/sizes slice; container nodes have a nil Pane.
type LayoutNode struct {
	Leaf   bool
	Pane   *pane.Pane // non-nil only if Leaf
	Dir    SplitDir   // meaningful only if !Leaf
	Parent *LayoutNode

	children []*LayoutNode
	sizes    []int // proportional sizes for flex layout
}

// AddChild appends a child to a container node.
//
// The child records this node as its Parent and receives the given
// proportional flex size; the existing sizes are left unchanged.
func (n *LayoutNode) AddChild(child *LayoutNode, size int) {
	child.Parent = n
	n.children = append(n.children, child)
	n.sizes = append(n.sizes, size)
}

// RemoveChild removes a child from a container node.
//
// The child's Parent is cleared, and both the children and matching size
// entries are removed. If child is not present the node is unchanged.
func (n *LayoutNode) RemoveChild(child *LayoutNode) {
	for i, c := range n.children {
		if c == child {
			n.children = append(n.children[:i], n.children[i+1:]...)
			n.sizes = append(n.sizes[:i], n.sizes[i+1:]...)
			child.Parent = nil
			return
		}
	}
}

// Children returns the children slice (read-only).
func (n *LayoutNode) Children() []*LayoutNode { return n.children }

// Sizes returns the proportional sizes slice (read-only).
func (n *LayoutNode) Sizes() []int { return n.sizes }

// ReplaceChild replaces oldChild with newChild in the parent container.
//
// The new child adopts the container as its Parent; oldChild is detached. The
// proportional sizes are kept as-is (the replacement inherits the old slot's
// weight). If oldChild is not found the tree is unchanged.
func (n *LayoutNode) ReplaceChild(oldChild, newChild *LayoutNode) {
	for i, c := range n.children {
		if c == oldChild {
			n.children[i] = newChild
			newChild.Parent = n
			oldChild.Parent = nil
			return
		}
	}
}

// Window holds the saved layout state for a single window (workspace).
// Only the active window's state lives directly on the WindowManager fields;
// all windows have a matching entry in the windows slice (the active one at
// currentWin) that acts as the save slot.
type Window struct {
	name      string
	root      *LayoutNode
	focused   *pane.Pane
	paneCount int
	zoomed    *pane.Pane
}

// WindowManager manages the tree of panes and positions them on the screen.
// It supports multiple windows (workspaces); only one is active at a time.
// The active window's state lives directly on the WindowManager fields
// (root, focused, etc.) so all existing code works unchanged; the windows
// slice holds one Window per window (the entry at currentWin mirrors the
// active state and is refreshed on every switch).
type WindowManager struct {
	root      *LayoutNode
	focused   *pane.Pane
	paneCount int // monotonically increasing for unique labels
	zoomed    *pane.Pane

	windows    []*Window // one entry per window; active one at currentWin
	currentWin int       // index of the active window in windows
	windowName string    // name of the currently active window

	gap     int // gutter (in cells) between adjacent panes, also the outer margin basis
	inset   int // per-pane inset on every side (derived from gap)
	overlap int // span overlap on shared split edges (derived from gap)
}

// NewWindowManager creates a new WindowManager.
//
// The tree starts empty; the initial pane is created lazily by Draw (or by the
// caller calling NewPane directly). A default window "1" is created. The pane
// gap defaults to one cell (see SetPaneGap).
func NewWindowManager() *WindowManager {
	wm := &WindowManager{
		windows:    []*Window{{name: "1"}},
		currentWin: 0,
		windowName: "1",
	}
	wm.SetPaneGap(1)
	return wm
}

// PaneGap returns the configured gutter width (in cells) between panes.
func (wm *WindowManager) PaneGap() int { return wm.gap }

// SetPaneGap configures the gutter between adjacent panes in cells (0 means
// panes share a border). The per-pane inset and the shared-edge span overlap
// are derived from it so the visible gutter lands exactly on the requested
// width while the outer margin matches the inset.
func (wm *WindowManager) SetPaneGap(g int) {
	wm.gap, wm.inset, wm.overlap = gapGeometry(g)
}

// FocusedPane returns the currently focused pane, or nil.
func (wm *WindowManager) FocusedPane() *pane.Pane { return wm.focused }

// PaneCount returns the total number of live panes.
//
// Counts the leaves of the tree (excluding empty/nil leaves) by walking it.
func (wm *WindowManager) PaneCount() int {
	count := 0
	wm.walk(wm.root, func(n *LayoutNode) {
		if n.Leaf && n.Pane != nil {
			count++
		}
	})
	return count
}

// PaneIndex returns the 1-based index of the given pane in traversal order.
//
// Used by the status bar ("pane: 2/5") and by prefix-number jumps. Returns -1
// when the pane is not in the tree.
func (wm *WindowManager) PaneIndex(p *pane.Pane) int {
	idx := 0
	found := -1
	wm.walk(wm.root, func(n *LayoutNode) {
		if n.Leaf {
			idx++
			if n.Pane == p {
				found = idx
			}
		}
	})
	return found
}

// collectPanes returns all leaf panes in tree order.
func (wm *WindowManager) collectPanes() []*pane.Pane {
	var out []*pane.Pane
	wm.walk(wm.root, func(n *LayoutNode) {
		if n.Leaf && n.Pane != nil {
			out = append(out, n.Pane)
		}
	})
	return out
}

// CollectPanes returns all leaf panes in tree order (public).
//
// Depth-first traversal order matches the user's visual left-to-right /
// top-to-bottom reading order, so pane N lines up with the Nth split.
func (wm *WindowManager) CollectPanes() []*pane.Pane { return wm.collectPanes() }

// CollectAllPanes returns every leaf pane across all windows, in window
// order then traversal order. Used at shutdown to close every PTY.
func (wm *WindowManager) CollectAllPanes() []*pane.Pane {
	var out []*pane.Pane
	for i, w := range wm.windows {
		root := w.root
		if i == wm.currentWin {
			root = wm.root
		}
		if root == nil {
			continue
		}
		wm.walk(root, func(n *LayoutNode) {
			if n.Leaf && n.Pane != nil {
				out = append(out, n.Pane)
			}
		})
	}
	return out
}

// walk traverses the tree depth-first, calling fn on every node.
//
// A nil tree visits nothing; children are visited after their parent so callers
// receive both containers and leaves unless they filter by n.Leaf.
func (wm *WindowManager) walk(n *LayoutNode, fn func(*LayoutNode)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.children {
		wm.walk(c, fn)
	}
}

// findLeaf returns the leaf node that contains the given pane.
//
// Returns nil when the pane is not in the tree (e.g. already closed).
func (wm *WindowManager) findLeaf(p *pane.Pane) *LayoutNode {
	var found *LayoutNode
	wm.walk(wm.root, func(n *LayoutNode) {
		if n.Leaf && n.Pane == p {
			found = n
		}
	})
	return found
}

// childIndex returns the index of child in n, or -1.
func (wm *WindowManager) childIndex(n *LayoutNode, child *LayoutNode) int {
	for i, c := range n.children {
		if c == child {
			return i
		}
	}
	return -1
}

// WindowCount returns the total number of windows.
func (wm *WindowManager) WindowCount() int { return len(wm.windows) }

// WindowIndex returns the 1-based index of the active window.
func (wm *WindowManager) WindowIndex() int { return wm.currentWin + 1 }

// WindowName returns the name of the active window.
func (wm *WindowManager) WindowName() string { return wm.windowName }

// WindowNames returns the names of all windows in order.
func (wm *WindowManager) WindowNames() []string {
	names := make([]string, len(wm.windows))
	for i, w := range wm.windows {
		names[i] = w.name
	}
	return names
}

// saveCurrent stores the active window's state into its save slot in the
// windows slice.
func (wm *WindowManager) saveCurrent() {
	w := wm.windows[wm.currentWin]
	w.root = wm.root
	w.focused = wm.focused
	w.paneCount = wm.paneCount
	w.zoomed = wm.zoomed
}

// switchTo saves the active window, then activates the window at the given
// index, restoring its layout state.
func (wm *WindowManager) switchTo(idx int) {
	if idx < 0 || idx >= len(wm.windows) {
		return
	}
	wm.saveCurrent()
	w := wm.windows[idx]
	wm.root = w.root
	wm.focused = w.focused
	wm.paneCount = w.paneCount
	wm.zoomed = w.zoomed
	wm.windowName = w.name
	wm.currentWin = idx
}

// NewWindow creates a new window after the current one and switches to it.
//
// The new window starts empty and is named one higher than the current
// highest window number. A fresh pane is created for it immediately (both
// horizontal and vertical splits now split that pane).
func (wm *WindowManager) NewWindow() error {
	wm.saveCurrent()

	// Determine the next name: one above the highest existing number so
	// names never collide even after windows are closed.
	next := 1
	for _, w := range wm.windows {
		var n int
		if _, err := fmt.Sscanf(w.name, "%d", &n); err == nil && n >= next {
			next = n + 1
		}
	}

	insertAt := wm.currentWin + 1
	w := &Window{name: fmt.Sprintf("%d", next)}
	wm.windows = append(wm.windows, nil)
	copy(wm.windows[insertAt+1:], wm.windows[insertAt:])
	wm.windows[insertAt] = w

	// Switch to the new (empty) window.
	wm.currentWin = insertAt
	wm.windowName = w.name
	wm.root = nil
	wm.focused = nil
	wm.zoomed = nil
	wm.paneCount = 0

	// Create the initial pane for the new window.
	if err := wm.NewPane(); err != nil {
		return err
	}
	return nil
}

// NextWindow switches to the next window (wrapping around). No-op when there
// is only one window.
func (wm *WindowManager) NextWindow() {
	if len(wm.windows) <= 1 {
		return
	}
	wm.switchTo((wm.currentWin + 1) % len(wm.windows))
}

// PrevWindow switches to the previous window (wrapping around).
func (wm *WindowManager) PrevWindow() {
	if len(wm.windows) <= 1 {
		return
	}
	wm.switchTo((wm.currentWin - 1 + len(wm.windows)) % len(wm.windows))
}

// windowGone removes the (now empty) active window and switches to a
// neighbor. It returns true when no windows are left (the app should quit).
//
// Called by closeLeaf when the last pane in the active window is closed.
func (wm *WindowManager) windowGone() bool {
	if len(wm.windows) == 1 {
		wm.root = nil
		wm.focused = nil
		wm.zoomed = nil
		return true
	}
	idx := wm.currentWin
	wm.windows = append(wm.windows[:idx], wm.windows[idx+1:]...)
	wm.root = nil
	wm.focused = nil
	wm.zoomed = nil
	wm.paneCount = 0

	if idx >= len(wm.windows) {
		idx = len(wm.windows) - 1
	}
	w := wm.windows[idx]
	wm.root = w.root
	wm.focused = w.focused
	wm.paneCount = w.paneCount
	wm.zoomed = w.zoomed
	wm.windowName = w.name
	wm.currentWin = idx
	return false
}
