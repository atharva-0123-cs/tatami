// Creating new panes by splitting the focused pane.
package layout

import (
	"fmt"
	"log"

	"github.com/atharva-0123-cs/tview-demo/pane"
)

// NewPane creates a new pane. If the tree is empty it becomes the root;
// otherwise it splits the currently focused pane horizontally (top/bottom).
//
// The pane label is derived from the monotonic paneCount so pane numbers never
// repeat even after closes, and the new pane takes focus.
func (wm *WindowManager) NewPane() error {
	wm.paneCount++
	label := fmt.Sprintf("%d", wm.paneCount)
	p, err := pane.NewPane(label)
	if err != nil {
		log.Printf("Failed to create pane: %v", err)
		return err
	}

	if wm.root == nil {
		wm.root = &LayoutNode{Leaf: true, Pane: p}
		wm.focused = p
		return nil
	}

	wm.splitCurrent(p, SplitHorizontal)
	return nil
}

// NewPaneVertical creates a new pane by splitting the focused pane vertically.
//
// Same as NewPane but the focused pane is split left/right instead of
// top/bottom.
func (wm *WindowManager) NewPaneVertical() error {
	wm.paneCount++
	label := fmt.Sprintf("%d", wm.paneCount)
	p, err := pane.NewPane(label)
	if err != nil {
		log.Printf("Failed to create pane: %v", err)
		return err
	}

	if wm.root == nil {
		wm.root = &LayoutNode{Leaf: true, Pane: p}
		wm.focused = p
		return nil
	}

	wm.splitCurrent(p, SplitVertical)
	return nil
}

// splitCurrent splits the currently focused pane, inserting newPane as a sibling.
//
// Three cases:
//   - the focused leaf is the root: wrap both in a new container of the given
//     direction, making it the root;
//   - the focused leaf's parent already splits in the same direction: insert
//     the new leaf directly after it in that parent;
//   - otherwise: replace the focused leaf with a fresh container (newly
//     splitting in the requested direction) holding the two leaves.
//
// The new pane always becomes focused and the two siblings start with equal
// size weights.
func (wm *WindowManager) splitCurrent(newPane *pane.Pane, dir SplitDir) {
	focusedNode := wm.findLeaf(wm.focused)
	if focusedNode == nil {
		log.Printf("splitCurrent: focused pane not found in tree")
		return
	}

	parent := focusedNode.Parent
	newLeaf := &LayoutNode{Leaf: true, Pane: newPane}

	if parent == nil {
		container := &LayoutNode{Leaf: false, Dir: dir}
		container.AddChild(focusedNode, 1)
		container.AddChild(newLeaf, 1)
		wm.root = container
	} else if parent.Dir == dir {
		idx := wm.childIndex(parent, focusedNode)
		parent.children = append(parent.children[:idx+1], append([]*LayoutNode{newLeaf}, parent.children[idx+1:]...)...)
		parent.sizes = append(parent.sizes[:idx+1], append([]int{1}, parent.sizes[idx+1:]...)...)
		newLeaf.Parent = parent
	} else {
		container := &LayoutNode{Leaf: false, Dir: dir}
		container.AddChild(focusedNode, 1)
		container.AddChild(newLeaf, 1)
		parent.ReplaceChild(focusedNode, container)
	}

	wm.focused = newPane
}
