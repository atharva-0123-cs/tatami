// Package layout implements the WindowManager: a tree of window.Nodes (each
// leaf holds one Pane, each container splits its children in one direction)
// plus focus, zoom, split, close, swap, rotate and resize operations. Every
// render pass walks the tree once to assign each pane a screen rectangle,
// draws each pane into the tcell screen, and paints the status bar.
//
// The implementation is split across files by concern: draw.go renders the
// layout and status bar, splits.go creates panes, close.go removes them,
// focus.go moves focus, transform.go zooms/swaps/rotates, and resize.go
// adjusts proportions. This file holds the WindowManager and its window/state
// bookkeeping; the tree primitives and workspace save slots live in the
// window package.
package layout

import (
	"fmt"

	"github.com/atharva-0123-cs/tatami/pane"
	"github.com/atharva-0123-cs/tatami/window"
)

// StatusBarHeight is the number of terminal rows reserved for the status line.
const StatusBarHeight = 1

// WindowManager manages the tree of panes and positions them on the screen.
// It supports multiple windows (workspaces); only one is active at a time.
//
// The active window's state lives directly on the WindowManager fields
// (root, focused, etc.) so all the layout algorithms work unchanged; the
// embedded window.Registry holds one Window save slot per window, and the
// entry at CurrentWin mirrors the active state and is refreshed on every
// switch.
type WindowManager struct {
	root      *window.Node
	focused   *pane.Pane
	paneCount int // monotonically increasing for unique labels
	zoomed    *pane.Pane

	window.Registry // ordered list of windows; active one at CurrentWin

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
		Registry: *window.NewRegistry(),
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
	wm.walk(wm.root, func(n *window.Node) {
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
	wm.walk(wm.root, func(n *window.Node) {
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
	wm.walk(wm.root, func(n *window.Node) {
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
	for i, w := range wm.Windows {
		root := w.Root
		if i == wm.CurrentWin {
			root = wm.root
		}
		if root == nil {
			continue
		}
		wm.walk(root, func(n *window.Node) {
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
func (wm *WindowManager) walk(n *window.Node, fn func(*window.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		wm.walk(c, fn)
	}
}

// findLeaf returns the leaf node that contains the given pane.
//
// Returns nil when the pane is not in the tree (e.g. already closed).
func (wm *WindowManager) findLeaf(p *pane.Pane) *window.Node {
	var found *window.Node
	wm.walk(wm.root, func(n *window.Node) {
		if n.Leaf && n.Pane == p {
			found = n
		}
	})
	return found
}

// childIndex returns the index of child in n, or -1.
func (wm *WindowManager) childIndex(n *window.Node, child *window.Node) int {
	for i, c := range n.Children {
		if c == child {
			return i
		}
	}
	return -1
}

// WindowCount returns the total number of windows.
func (wm *WindowManager) WindowCount() int { return wm.Count() }

// WindowIndex returns the 1-based index of the active window.
func (wm *WindowManager) WindowIndex() int { return wm.Index() + 1 }

// WindowName returns the name of the active window.
func (wm *WindowManager) WindowName() string { return wm.Name() }

// WindowNames returns the names of all windows in order.
func (wm *WindowManager) WindowNames() []string { return wm.Names() }

// saveCurrent stores the active window's state into its save slot in the
// Windows slice.
func (wm *WindowManager) saveCurrent() {
	w := wm.Active()
	w.Root = wm.root
	w.Focused = wm.focused
	w.PaneCount = wm.paneCount
	w.Zoomed = wm.zoomed
}

// switchTo saves the active window, then activates the window at the given
// index, restoring its layout state.
func (wm *WindowManager) switchTo(idx int) {
	if idx < 0 || idx >= len(wm.Windows) {
		return
	}
	wm.saveCurrent()
	w := wm.Windows[idx]
	wm.root = w.Root
	wm.focused = w.Focused
	wm.paneCount = w.PaneCount
	wm.zoomed = w.Zoomed
	wm.Registry.WindowName = w.Name
	wm.CurrentWin = idx
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
	for _, w := range wm.Windows {
		var n int
		if _, err := fmt.Sscanf(w.Name, "%d", &n); err == nil && n >= next {
			next = n + 1
		}
	}

	insertAt := wm.CurrentWin + 1
	w := &window.Window{Name: fmt.Sprintf("%d", next)}
	wm.Windows = append(wm.Windows, nil)
	copy(wm.Windows[insertAt+1:], wm.Windows[insertAt:])
	wm.Windows[insertAt] = w

	// Switch to the new (empty) window.
	wm.CurrentWin = insertAt
	wm.Registry.WindowName = w.Name
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
	if len(wm.Windows) <= 1 {
		return
	}
	wm.switchTo((wm.CurrentWin + 1) % len(wm.Windows))
}

// PrevWindow switches to the previous window (wrapping around).
func (wm *WindowManager) PrevWindow() {
	if len(wm.Windows) <= 1 {
		return
	}
	wm.switchTo((wm.CurrentWin - 1 + len(wm.Windows)) % len(wm.Windows))
}

// windowGone removes the (now empty) active window and switches to a
// neighbor. It returns true when no windows are left (the app should quit).
//
// Called by closeLeaf when the last pane in the active window is closed.
func (wm *WindowManager) windowGone() bool {
	if len(wm.Windows) == 1 {
		wm.root = nil
		wm.focused = nil
		wm.zoomed = nil
		return true
	}
	idx := wm.CurrentWin
	wm.Windows = append(wm.Windows[:idx], wm.Windows[idx+1:]...)
	wm.root = nil
	wm.focused = nil
	wm.zoomed = nil
	wm.paneCount = 0

	if idx >= len(wm.Windows) {
		idx = len(wm.Windows) - 1
	}
	w := wm.Windows[idx]
	wm.root = w.Root
	wm.focused = w.Focused
	wm.paneCount = w.PaneCount
	wm.zoomed = w.Zoomed
	wm.Registry.WindowName = w.Name
	wm.CurrentWin = idx
	return false
}
