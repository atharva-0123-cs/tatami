// Package layout implements the WindowManager: a tree of LayoutNodes (each
// leaf holds one Pane, each container splits its children in one direction)
// plus focus, zoom, split, close, swap, rotate and resize operations. Every
// render pass walks the tree once to assign each pane a screen rectangle,
// draws each pane into the tcell screen, and paints the status bar.
package layout

import (
	"fmt"
	"log"

	"github.com/gdamore/tcell/v2"

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

// Draw lays out and draws all panes and the status bar into the screen.
//
// Steps:
//  1. Lazily create the first pane if the tree is empty, unless the tree is
//     completely gone (then only the status bar is drawn).
//  2. Reserve StatusBarHeight rows at the bottom for the status bar.
//  3. Iterate the tree with distribute to assign rectangles and draw every
//     pane (a zoomed pane draws alone at full size).
//  4. Collect the focused pane's logical cursor and show/hide it via
//     ShowCursor/HideCursor.
//  5. renderStatus paints the bottom line.
//
// Draw must run on the main goroutine (it touches the tcell screen buffer and,
// inside each Pane.Draw, resizes PTYs).
func (wm *WindowManager) Draw(screen tcell.Screen, width, height int, prefixActive, resizeMode bool, errorMsg string, style StatusStyle) {
	if width <= 0 || height <= 0 {
		return
	}

	if wm.root == nil && wm.PaneCount() == 0 {
		if err := wm.NewPane(); err != nil {
			log.Printf("failed to create initial pane: %v", err)
			return
		}
	}

	if wm.root == nil {
		wm.renderStatus(screen, width, height, prefixActive, resizeMode, errorMsg, style)
		screen.HideCursor()
		return
	}

	paneRegionH := height - StatusBarHeight
	if paneRegionH < 1 {
		paneRegionH = 1
	}

	var cursorX, cursorY int
	var cursorVisible bool

	if wm.zoomed != nil {
		isFocused := (wm.zoomed == wm.focused)
		cx, cy, hasCursor := wm.zoomed.Draw(0, 0, width, paneRegionH, isFocused, screen)
		if isFocused && hasCursor {
			cursorX, cursorY = cx, cy
			cursorVisible = true
		}
	} else {
		cx, cy, hasCursor := wm.distribute(wm.root, 0, 0, width, paneRegionH, screen)
		if hasCursor {
			cursorX, cursorY = cx, cy
			cursorVisible = true
		}
	}

	wm.renderStatus(screen, width, height, prefixActive, resizeMode, errorMsg, style)

	if cursorVisible {
		screen.ShowCursor(cursorX, cursorY)
	} else {
		screen.HideCursor()
	}
}

// distribute recursively assigns rectangles to the nodes of the tree.
//
// Leaves draw their pane (Pane.Draw) when their rectangle is non-empty and
// report whether the focused pane's cursor landed there. Containers partition
// their rectangle along their SplitDir proportionally to each child's size
// weight: horizontal splits stack children top-to-bottom (each child gets a
// band of height avail*size/total, the last child absorbing rounding
// remainder), vertical splits lay children out left-to-right the same way for
// widths. The focused pane's cursor coordinates bubble up to Draw.
//
// distribute returns (cursorX, cursorY, hasCursor); hasCursor is true only for
// the focused pane's draw when its cursor is visible.
func (wm *WindowManager) distribute(n *LayoutNode, x0, y0, x1, y1 int, screen tcell.Screen) (cursorX, cursorY int, hasCursor bool) {
	if n == nil {
		return 0, 0, false
	}
	if n.Leaf {
		if n.Pane == nil {
			return 0, 0, false
		}
		isFocused := (n.Pane == wm.focused)
		cx, cy, hc := n.Pane.Draw(x0, y0, x1, y1, isFocused, screen)
		if isFocused && hc {
			return cx, cy, true
		}
		return 0, 0, false
	}

	total := 0
	for _, s := range n.sizes {
		total += s
	}
	if total <= 0 {
		return 0, 0, false
	}

	if n.Dir == SplitHorizontal {
		avail := y1 - y0
		cursor := y0
		for i, child := range n.children {
			ch := (avail * n.sizes[i]) / total
			cy1 := cursor + ch
			if i == len(n.children)-1 {
				cy1 = y1
			}
			if cy1 > cursor {
				cx, cy, hc := wm.distribute(child, x0, cursor, x1, cy1, screen)
				if hc {
					cursorX, cursorY, hasCursor = cx, cy, true
				}
			}
			cursor = cy1
		}
	} else {
		avail := x1 - x0
		cursor := x0
		for i, child := range n.children {
			cw := (avail * n.sizes[i]) / total
			cx1 := cursor + cw
			if i == len(n.children)-1 {
				cx1 = x1
			}
			if cx1 > cursor {
				cx, cy, hc := wm.distribute(child, cursor, y0, cx1, y1, screen)
				if hc {
					cursorX, cursorY, hasCursor = cx, cy, true
				}
			}
			cursor = cx1
		}
	}
	return cursorX, cursorY, hasCursor
}

// statusDark is a dark-theme color scheme for the status bar: near-black
// background with light text; the bar turns yellow while a prefix/resize mode
// is active and red to surface an error.
const (
	statusBg  = tcell.Color(235) | tcell.ColorValid
	statusFg  = tcell.Color(252) | tcell.ColorValid
	statusHi  = tcell.Color(226) | tcell.ColorValid // bright yellow
	statusErr = tcell.Color(196) | tcell.ColorValid // red
)

// StatusStyle carries the per-run status-bar appearance. Fg and Bg may be set
// from the config file; when either is zero the dark-theme default is used.
type StatusStyle struct {
	PrefixLabel string // prefix name shown in the idle hint, e.g. "Ctrl+A"
	Fg          tcell.Color
	Bg          tcell.Color
}

// resolve applies the dark-theme defaults to any zero fields.
func (s StatusStyle) resolve() StatusStyle {
	if s.Fg == 0 {
		s.Fg = statusFg
	}
	if s.Bg == 0 {
		s.Bg = statusBg
	}
	return s
}

// renderStatus draws the status line at the bottom of the screen.
//
// The left half shows the mode banner (" ROGE | [PREFIX] ...", "[RESIZE] ..."
// or an error message) in the highlighted color while a mode/error is active;
// the right half shows the pane count and focused index (plus "[ZOOM]" when
// zoomed). The whole row is blanked first so stale glyphs never survive from a
// previous size. Must run on the main goroutine.
func (wm *WindowManager) renderStatus(screen tcell.Screen, maxX, maxY int, prefixActive, resizeMode bool, errorMsg string, style StatusStyle) {
	style = style.resolve()
	y := maxY - 1
	left := " ROGE "
	if errorMsg != "" {
		left += "| " + errorMsg + " "
	} else if resizeMode {
		left += "| [RESIZE] h/j/k/l to resize, Esc to cancel "
	} else if prefixActive {
		left += "| [PREFIX] c:h-split C:v-split x:close z:zoom 1-9:jump q:quit "
	} else {
		left += fmt.Sprintf("| Prefix: %s then commands... ", style.PrefixLabel)
	}

	total := wm.PaneCount()
	right := ""
	if total > 0 {
		right = fmt.Sprintf("panes: %d", total)
		if wm.focused != nil {
			right += fmt.Sprintf(" | pane: %d/%d", wm.PaneIndex(wm.focused), total)
		}
	}
	if wm.zoomed != nil {
		right += " [ZOOM]"
	}
	right += " "

	fg := style.Fg
	bg := style.Bg
	if errorMsg != "" {
		fg = statusErr
	} else if prefixActive || resizeMode {
		fg = statusHi
	}

	st := tcell.StyleDefault.Foreground(fg).Background(bg)
	if errorMsg != "" || prefixActive || resizeMode {
		st = st.Bold(true)
	}

	runesLeft := []rune(left)
	runesRight := []rune(right)

	for x := 0; x < maxX; x++ {
		screen.SetContent(x, y, ' ', nil, st)
	}

	for x := 0; x < len(runesLeft) && x < maxX; x++ {
		screen.SetContent(x, y, runesLeft[x], nil, st)
	}

	rightStart := maxX - len(runesRight)
	if rightStart > len(runesLeft) {
		for i, r := range runesRight {
			screen.SetContent(rightStart+i, y, r, nil, st)
		}
	}
}

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

// childIndex returns the index of child in n, or -1.
func (wm *WindowManager) childIndex(n *LayoutNode, child *LayoutNode) int {
	for i, c := range n.children {
		if c == child {
			return i
		}
	}
	return -1
}

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
