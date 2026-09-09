// Closing panes: removing a leaf from the layout tree, collapsing one-child
// containers, pruning empty parents, and refocusing a neighbor.
package layout

import (
	"log/slog"

	"github.com/atharva-0123-cs/tatami/pane"
	"github.com/atharva-0123-cs/tatami/window"
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

// CloseDeadPanes removes every pane whose shell process has exited, across all
// windows. It returns true if the last window was closed (the app should quit).
//
// Called at the top of every event-loop iteration; panes that were marked dead
// by their process-exit goroutine get closed and the tree is refocused so the
// UI always shows a live neighbor. Dead panes in inactive windows are cleaned
// up too by temporarily switching to each affected window.
func (wm *WindowManager) CloseDeadPanes() bool {
	type group struct {
		idx   int
		panes []*pane.Pane
	}
	var groups []group
	origIdx := wm.CurrentWin

	for i, w := range wm.Windows {
		// The active window's tree lives in wm.root; inactive windows keep
		// theirs in the saved record.
		root := w.Root
		if i == wm.CurrentWin {
			root = wm.root
		}
		if root == nil {
			continue
		}
		var panes []*pane.Pane
		wm.walk(root, func(n *window.Node) {
			if n.Leaf && n.Pane != nil && n.Pane.IsDead() {
				panes = append(panes, n.Pane)
			}
		})
		if len(panes) > 0 {
			groups = append(groups, group{i, panes})
		}
	}
	if len(groups) == 0 {
		return false
	}

	for _, g := range groups {
		// The window may have been removed during a previous group's sweep.
		if g.idx >= len(wm.Windows) {
			continue
		}
		wm.switchTo(g.idx)
		for _, p := range g.panes {
			if wm.closePane(p) {
				return true
			}
		}
	}

	// Restore the user's original window if it still exists.
	if origIdx < len(wm.Windows) {
		wm.switchTo(origIdx)
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
func (wm *WindowManager) closeLeaf(focusedNode *window.Node) bool {
	p := focusedNode.Pane
	if err := p.Close(); err != nil {
		slog.Error("failed to close pane", "pane", p.Name(), "err", err)
	}
	if wm.zoomed == p {
		wm.zoomed = nil
	}

	parent := focusedNode.Parent

	if parent == nil {
		// The last pane in the active window is gone. If other windows
		// exist, remove this window and switch to a neighbor; otherwise
		// the app quits.
		return wm.windowGone()
	}

	sibling := wm.pickSibling(focusedNode)

	parent.RemoveChild(focusedNode)

	for parent != nil && len(parent.Children) == 1 {
		grandparent := parent.Parent
		if grandparent == nil {
			wm.root = parent.Children[0]
			wm.root.Parent = nil
			break
		}
		grandparent.ReplaceChild(parent, parent.Children[0])
		parent = grandparent
	}

	if parent != nil && len(parent.Children) == 0 {
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
		return wm.windowGone()
	}
	return false
}

// pickSibling returns the nearest sibling to focus after closing a pane.
//
// Prefers the sibling after the closed one (matching the visual "next" slot),
// falling back to the one before it.
func (wm *WindowManager) pickSibling(n *window.Node) *window.Node {
	parent := n.Parent
	if parent == nil {
		return nil
	}
	idx := wm.childIndex(parent, n)
	if idx+1 < len(parent.Children) {
		return parent.Children[idx+1]
	}
	if idx-1 >= 0 {
		return parent.Children[idx-1]
	}
	return nil
}
