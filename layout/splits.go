// Creating new panes by splitting the focused pane.
package layout

import (
	"fmt"
	"log/slog"

	"github.com/atharva-0123-cs/tatami/pane"
	"github.com/atharva-0123-cs/tatami/window"
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
		slog.Warn("failed to create pane", "err", err)
		return err
	}

	if wm.root == nil {
		wm.root = &window.Node{Leaf: true, Pane: p}
		wm.focused = p
		return nil
	}

	wm.splitCurrent(p, window.SplitHorizontal)
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
		slog.Warn("failed to create vertical pane", "err", err)
		return err
	}

	if wm.root == nil {
		wm.root = &window.Node{Leaf: true, Pane: p}
		wm.focused = p
		return nil
	}

	wm.splitCurrent(p, window.SplitVertical)
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
func (wm *WindowManager) splitCurrent(newPane *pane.Pane, dir window.SplitDir) {
	focusedNode := wm.findLeaf(wm.focused)
	if focusedNode == nil {
		slog.Warn("splitCurrent: focused pane not found in tree")
		return
	}

	parent := focusedNode.Parent
	newLeaf := &window.Node{Leaf: true, Pane: newPane}

	if parent == nil {
		container := &window.Node{Leaf: false, Dir: dir}
		container.AddChild(focusedNode, 1)
		container.AddChild(newLeaf, 1)
		wm.root = container
	} else if parent.Dir == dir {
		idx := wm.childIndex(parent, focusedNode)
		parent.Children = append(parent.Children[:idx+1], append([]*window.Node{newLeaf}, parent.Children[idx+1:]...)...)
		parent.Sizes = append(parent.Sizes[:idx+1], append([]int{1}, parent.Sizes[idx+1:]...)...)
		newLeaf.Parent = parent
	} else {
		container := &window.Node{Leaf: false, Dir: dir}
		container.AddChild(focusedNode, 1)
		container.AddChild(newLeaf, 1)
		parent.ReplaceChild(focusedNode, container)
	}

	wm.focused = newPane
}
