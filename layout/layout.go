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
	"github.com/atharva-0123-cs/tview-demo/pane"
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
// Sibling rectangles within one container never overlap and partition the
// container's rectangle proportionally to their sizes. Leaf nodes have a nil
// children/sizes slice; container nodes have a nil Pane.
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

// WindowManager manages the tree of panes and positions them on the screen.
type WindowManager struct {
	root      *LayoutNode
	focused   *pane.Pane
	paneCount int // monotonically increasing for unique labels
	zoomed    *pane.Pane
}

// NewWindowManager creates a new WindowManager.
//
// The tree starts empty; the initial pane is created lazily by Draw (or by the
// caller calling NewPane directly).
func NewWindowManager() *WindowManager {
	return &WindowManager{}
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
