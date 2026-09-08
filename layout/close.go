// Closing panes: removing a leaf from the layout tree, collapsing one-child
// containers, pruning empty parents, and refocusing a neighbor.
package layout

import (
	"log"

	"github.com/atharva-0123-cs/tview-demo/pane"
)

// CloseCurrentPane removes the focused pane from the layout. It returns true
// if the last pane was closed (the app should quit).
func (wm *WindowManager) CloseCurrentPane() bool {
	if wm.root == nil {
		return false
	}
	focusedNode := wm.findLeaf(wm.focused)
	if focusedNode == nil {
		return false
	}
	return wm.closeLeaf(focusedNode)
}

// CloseDeadPanes removes every pane whose shell process has exited. It returns
// true if the last pane was closed (the app should quit).
//
// Called at the top of every event-loop iteration; panes that were marked dead
// by their process-exit goroutine get closed and the tree is refocused so the
// UI always shows a live neighbor.
func (wm *WindowManager) CloseDeadPanes() bool {
	if wm.root == nil {
		return false
	}
	for _, p := range wm.collectPanes() {
		if p.IsDead() {
			if wm.closePane(p) {
				return true
			}
		}
	}
	return false
}

// closePane removes a specific pane from the layout, returning true if the
// last pane was closed.
func (wm *WindowManager) closePane(p *pane.Pane) bool {
	if wm.root == nil || p == nil {
		return false
	}
	n := wm.findLeaf(p)
	if n == nil {
		return false
	}
	return wm.closeLeaf(n)
}

// closeLeaf removes the given leaf node from the layout tree, closes its pane,
// and re-focuses a neighbor. Returns true if the last pane was closed.
//
// After removal, single-child containers collapse upward and empty containers
// are pruned so the tree stays canonical. Focus moves to the sibling that
// occupied the adjacent slot (or the first remaining pane); when nothing is
// left the tree is cleared and true is returned.
func (wm *WindowManager) closeLeaf(focusedNode *LayoutNode) bool {
	p := focusedNode.Pane
	if err := p.Close(); err != nil {
		log.Printf("Error closing pane: %v", err)
	}
	if wm.zoomed == p {
		wm.zoomed = nil
	}

	parent := focusedNode.Parent

	if parent == nil {
		wm.root = nil
		wm.focused = nil
		return true
	}

	sibling := wm.pickSibling(focusedNode)

	parent.RemoveChild(focusedNode)

	for parent != nil && len(parent.children) == 1 {
		grandparent := parent.Parent
		if grandparent == nil {
			wm.root = parent.children[0]
			wm.root.Parent = nil
			break
		}
		grandparent.ReplaceChild(parent, parent.children[0])
		parent = grandparent
	}

	if parent != nil && len(parent.children) == 0 {
		if parent.Parent == nil {
			wm.root = nil
		} else {
			parent.Parent.RemoveChild(parent)
		}
	}

	if sibling != nil {
		wm.setFocus(sibling.Pane)
	} else {
		panes := wm.collectPanes()
		if len(panes) > 0 {
			wm.setFocus(panes[0])
			return false
		}
		wm.focused = nil
		return true
	}
	return false
}

// pickSibling returns the nearest sibling to focus after closing a pane.
//
// Prefers the sibling after the closed one (matching the visual "next" slot),
// falling back to the one before it.
func (wm *WindowManager) pickSibling(n *LayoutNode) *LayoutNode {
	parent := n.Parent
	if parent == nil {
		return nil
	}
	idx := wm.childIndex(parent, n)
	if idx+1 < len(parent.children) {
		return parent.children[idx+1]
	}
	if idx-1 >= 0 {
		return parent.children[idx-1]
	}
	return nil
}
