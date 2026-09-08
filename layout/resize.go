// Resizing panes: nudging a pane's proportional size weight within its parent
// container. The layout reflows on the next Draw pass.
package layout

// IncreaseSize and DecreaseSize adjust the proportion of the focused pane.
func (wm *WindowManager) IncreaseSize(dir SplitDir) { wm.adjustSize(dir, 1) }
func (wm *WindowManager) DecreaseSize(dir SplitDir) { wm.adjustSize(dir, -1) }

// adjustSize nudges the focused pane's size proportion by delta, floored at 1.
//
// Only applies when the focused pane's parent splits in the given direction
// (so left/right resize only works on vertical splits, up/down on horizontal
// ones). The sibling proportions stay fixed; the layout reflows on the next
// Draw. Sizes are floored at 1 so a pane can never disappear entirely.
func (wm *WindowManager) adjustSize(dir SplitDir, delta int) {
	if wm.focused == nil || wm.root == nil {
		return
	}
	focusedNode := wm.findLeaf(wm.focused)
	if focusedNode == nil || focusedNode.Parent == nil {
		return
	}
	parent := focusedNode.Parent
	if parent.Dir != dir {
		return
	}
	idx := wm.childIndex(parent, focusedNode)
	newSize := parent.sizes[idx] + delta
	if newSize < 1 {
		newSize = 1
	}
	parent.sizes[idx] = newSize
}
