// Rendering the pane layout and status bar into the tcell screen buffer.
// These functions run on the main goroutine (Draw also resizes PTYs via each
// pane's Draw).
package layout

import (
	"fmt"
	"log"

	"github.com/gdamore/tcell/v2"
)

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
// weight using seamSpans: horizontal splits stack children top-to-bottom (each
// child gets a band of height avail*size/total, the last child absorbing
// rounding remainder), vertical splits lay children out left-to-right the same
// way for widths, and every sibling after the first overlaps the previous one
// by one cell so the shared edge renders as a single border row/column. The
// focused pane's cursor coordinates bubble up to Draw.
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

	horizontal := n.Dir == SplitHorizontal
	avail, pos0 := 0, y0
	if horizontal {
		avail = y1 - y0
	} else {
		avail, pos0 = x1-x0, x0
	}

	for i, child := range n.children {
		sp := seamSpans(avail, n.sizes)[i]
		if sp[1] <= sp[0] {
			continue
		}
		if horizontal {
			cx, cy, hc := wm.distribute(child, x0, pos0+sp[0], x1, pos0+sp[1], screen)
			if hc {
				cursorX, cursorY, hasCursor = cx, cy, true
			}
		} else {
			cx, cy, hc := wm.distribute(child, pos0+sp[0], y0, pos0+sp[1], y1, screen)
			if hc {
				cursorX, cursorY, hasCursor = cx, cy, true
			}
		}
	}
	return cursorX, cursorY, hasCursor
}

// seamSpans computes each child's [start, end) span along a split axis given
// the available length and proportional sizes.
//
// Children are laid out proportionally (span = avail*size/total, the last child
// absorbing the rounding remainder). Each child after the first overlaps the
// previous child by one cell on the shared edge: the new child starts one cell
// *before* where the previous child ended, so the two panes' touching borders
// land on the same row/column and render as a single divider. Spans are clamped
// to never start before the axis origin.
func seamSpans(avail int, sizes []int) [][2]int {
	total := 0
	for _, s := range sizes {
		total += s
	}
	spans := make([][2]int, len(sizes))
	if total <= 0 {
		return spans
	}
	cursor := 0
	for i, s := range sizes {
		end := cursor + (avail*s)/total
		if i == len(sizes)-1 {
			end = avail
		}
		start := cursor
		if i > 0 {
			start = cursor - 1
			if start < 0 {
				start = 0
			}
		}
		spans[i] = [2]int{start, end}
		cursor = end
	}
	return spans
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
