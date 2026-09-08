// Structural transforms of a pane's parent container: zoom, sibling swap and
// rotation. All operate on the focused pane's parent node.
package layout

// Zoom toggles zoom on the current pane.
//
// Zoomed panes are drawn alone at the full pane region (distribute is
// bypassed), which effectively maximizes that pane while preserving the rest
// of the tree. Zooming again un-zooms.
func (wm *WindowManager) Zoom() {
	if wm.focused == nil {
		return
	}
	if wm.zoomed == wm.focused {
		wm.zoomed = nil
		return
	}
	wm.zoomed = wm.focused
}

// SwapNext and SwapPrev swap the current pane with its sibling.
func (wm *WindowManager) SwapNext() { wm.swapPane(1) }
func (wm *WindowManager) SwapPrev() { wm.swapPane(-1) }

// swapPane swaps the focused pane's tree node with its neighbor `delta`
// positions away in the same parent (+1 next, -1 previous).
//
// Only the node and its size weight swap places; focus stays with the
// (now relocated) pane. No-ops when the neighbor is out of range.
func (wm *WindowManager) swapPane(delta int) {
	if wm.focused == nil || wm.root == nil {
		return
	}
	focusedNode := wm.findLeaf(wm.focused)
	if focusedNode == nil || focusedNode.Parent == nil {
		return
	}
	parent := focusedNode.Parent
	idx := wm.childIndex(parent, focusedNode)
	target := idx + delta
	if target < 0 || target >= len(parent.children) {
		return
	}
	parent.children[idx], parent.children[target] = parent.children[target], parent.children[idx]
	parent.sizes[idx], parent.sizes[target] = parent.sizes[target], parent.sizes[idx]
}

// RotateFocused rotates all siblings in the parent container by one position.
//
// delta > 0 rotates right (last sibling becomes first), delta < 0 rotates
// left. Size weights travel with their siblings. No-op for containers with
// fewer than two children.
func (wm *WindowManager) RotateFocused(delta int) {
	if wm.focused == nil || wm.root == nil {
		return
	}
	focusedNode := wm.findLeaf(wm.focused)
	if focusedNode == nil || focusedNode.Parent == nil {
		return
	}
	parent := focusedNode.Parent
	n := len(parent.children)
	if n < 2 {
		return
	}
	if delta > 0 {
		last := parent.children[n-1]
		copy(parent.children[1:], parent.children[:n-1])
		parent.children[0] = last
		lastSize := parent.sizes[n-1]
		copy(parent.sizes[1:], parent.sizes[:n-1])
		parent.sizes[0] = lastSize
	} else {
		first := parent.children[0]
		copy(parent.children[:n-1], parent.children[1:])
		parent.children[n-1] = first
		firstSize := parent.sizes[0]
		copy(parent.sizes[:n-1], parent.sizes[1:])
		parent.sizes[n-1] = firstSize
	}
}
