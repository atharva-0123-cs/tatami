// Focus movement: moving the focused pane in a split direction, jump-to-pane,
// and the helpers that resolve tree positions for focus.
package layout

import (
	"github.com/atharva-0123-cs/tatami/pane"
	"github.com/atharva-0123-cs/tatami/window"
)

// firstPane returns the first leaf pane in a subtree.
func (wm *WindowManager) firstPane(n *window.Node) *pane.Pane {
	if n.Leaf {
		return n.Pane
	}
	for _, c := range n.Children {
		if p := wm.firstPane(c); p != nil {
			return p
		}
	}
	return nil
}

// MoveFocus moves focus in the given direction relative to the current pane.
//
// Scans outward from the focused leaf's ancestors: the closest ancestor whose
// split direction matches dir offers a sibling slot delta positions away
// (SplitHorizontal: up/down; SplitVertical: left/right). Focus moves to the
// first pane inside that sibling, so crossing is always to the "nearest"
// neighbor in that direction.
func (wm *WindowManager) MoveFocus(dir window.SplitDir, delta int) {
	if wm.focused == nil || wm.root == nil {
		return
	}
	focusedNode := wm.findLeaf(wm.focused)
	if focusedNode == nil || focusedNode.Parent == nil {
		return
	}

	current := focusedNode
	for current.Parent != nil {
		parent := current.Parent
		if parent.Dir == dir {
			idx := wm.childIndex(parent, current)
			next := idx + delta
			if next >= 0 && next < len(parent.Children) {
				if first := wm.firstPane(parent.Children[next]); first != nil {
					wm.setFocus(first)
					return
				}
			}
		}
		current = parent
	}
}

// setFocus changes the focused pane.
func (wm *WindowManager) setFocus(p *pane.Pane) {
	wm.focused = p
}

// FocusPane sets focus to a specific pane.
func (wm *WindowManager) FocusPane(p *pane.Pane) {
	if p == nil {
		return
	}
	wm.setFocus(p)
}

// FocusLeft/FocusRight move focus between side-by-side panes (SplitVertical).
func (wm *WindowManager) FocusLeft()  { wm.MoveFocus(window.SplitVertical, -1) }
func (wm *WindowManager) FocusRight() { wm.MoveFocus(window.SplitVertical, 1) }

// FocusUp/FocusDown move focus between stacked panes (SplitHorizontal).
func (wm *WindowManager) FocusUp()   { wm.MoveFocus(window.SplitHorizontal, -1) }
func (wm *WindowManager) FocusDown() { wm.MoveFocus(window.SplitHorizontal, 1) }

// FocusAt sets focus to the pane at screen coordinates (px, py).
//
// Walks the layout tree computing each leaf's rectangle (matching distribute's
// logic) and returns the first pane whose inset rectangle contains the point.
// Returns true when a pane was found and focused.
func (wm *WindowManager) FocusAt(px, py, screenW, screenH int) bool {
	if wm.root == nil {
		return false
	}
	paneRegionH := screenH - StatusBarHeight
	if paneRegionH < 1 {
		paneRegionH = 1
	}
	if wm.zoomed != nil {
		wm.setFocus(wm.zoomed)
		return true
	}
	found := wm.findPaneAt(wm.root, 0, 0, screenW, paneRegionH, px, py)
	if found != nil {
		wm.setFocus(found)
		return true
	}
	return false
}

// findPaneAt recursively walks the tree to find which pane occupies (px, py).
func (wm *WindowManager) findPaneAt(n *window.Node, x0, y0, x1, y1, px, py int) *pane.Pane {
	if n == nil {
		return nil
	}
	if n.Leaf {
		if n.Pane == nil {
			return nil
		}
		ix0 := x0 + wm.inset
		iy0 := y0 + wm.inset
		ix1 := x1 - wm.inset
		iy1 := y1 - wm.inset
		if ix1 <= ix0 || iy1 <= iy0 {
			return nil
		}
		if px >= ix0 && px < ix1 && py >= iy0 && py < iy1 {
			return n.Pane
		}
		return nil
	}
	total := 0
	for _, s := range n.Sizes {
		total += s
	}
	if total <= 0 {
		return nil
	}
	horizontal := n.Dir == window.SplitHorizontal
	avail, pos0 := 0, y0
	if horizontal {
		avail = y1 - y0
	} else {
		avail, pos0 = x1-x0, x0
	}
	for i, child := range n.Children {
		sp := wm.seamSpans(avail, n.Sizes)[i]
		if sp[1] <= sp[0] {
			continue
		}
		if horizontal {
			if p := wm.findPaneAt(child, x0, pos0+sp[0], x1, pos0+sp[1], px, py); p != nil {
				return p
			}
		} else {
			if p := wm.findPaneAt(child, pos0+sp[0], y0, pos0+sp[1], y1, px, py); p != nil {
				return p
			}
		}
	}
	return nil
}
