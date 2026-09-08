// Focus movement: moving the focused pane in a split direction, jump-to-pane,
// and the helpers that resolve tree positions for focus.
package layout

import (
	"github.com/atharva-0123-cs/tview-demo/pane"
)

// firstPane returns the first leaf pane in a subtree.
func (wm *WindowManager) firstPane(n *LayoutNode) *pane.Pane {
	if n.Leaf {
		return n.Pane
	}
	for _, c := range n.children {
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
func (wm *WindowManager) MoveFocus(dir SplitDir, delta int) {
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
			if next >= 0 && next < len(parent.children) {
				if first := wm.firstPane(parent.children[next]); first != nil {
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
func (wm *WindowManager) FocusLeft()  { wm.MoveFocus(SplitVertical, -1) }
func (wm *WindowManager) FocusRight() { wm.MoveFocus(SplitVertical, 1) }

// FocusUp/FocusDown move focus between stacked panes (SplitHorizontal).
func (wm *WindowManager) FocusUp()   { wm.MoveFocus(SplitHorizontal, -1) }
func (wm *WindowManager) FocusDown() { wm.MoveFocus(SplitHorizontal, 1) }
