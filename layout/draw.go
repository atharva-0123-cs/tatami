// Rendering the pane layout and status bar into the tcell screen buffer.
// These functions run on the main goroutine (Draw also resizes PTYs via each
// pane's Draw).
package layout

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/atharva-0123-cs/tatami/window"
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
func (wm *WindowManager) Draw(screen tcell.Screen, width, height int, prefixActive, resizeMode, copyMode bool, errorMsg string, style StatusStyle) {
	if width <= 0 || height <= 0 {
		return
	}

	if wm.root == nil && wm.PaneCount() == 0 {
		if err := wm.NewPane(); err != nil {
			slog.Warn("failed to create initial pane", "err", err)
			return
		}
	}

	if wm.root == nil {
		wm.renderStatus(screen, width, height, prefixActive, resizeMode, copyMode, errorMsg, style)
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
		cx, cy, hasCursor := wm.distribute(wm.root, 0, 0, width, paneRegionH, width, paneRegionH, screen)
		if hasCursor {
			cursorX, cursorY = cx, cy
			cursorVisible = true
		}
	}

	wm.renderStatus(screen, width, height, prefixActive, resizeMode, copyMode, errorMsg, style)

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
// way for widths. Leaves inset their assigned rectangle on internal sides by
// the window's gap inset (and on screen-edge sides by the smaller outer margin)
// so adjacent panes are separated by a thin gutter while staying close to the
// screen edges. The focused pane's cursor coordinates bubble up to Draw.
//
// distribute returns (cursorX, cursorY, hasCursor); hasCursor is true only for
// the focused pane's draw when its cursor is visible.
func (wm *WindowManager) distribute(n *window.Node, x0, y0, x1, y1, regionW, regionH int, screen tcell.Screen) (cursorX, cursorY int, hasCursor bool) {
	if n == nil {
		return 0, 0, false
	}
	if n.Leaf {
		if n.Pane == nil {
			return 0, 0, false
		}
		// Inset to create visible separation between adjacent panes; sides on
		// the screen edge use the smaller outer margin.
		left, top, right, bottom := wm.edgeInsets(x0, y0, x1, y1, regionW, regionH)
		ix0 := x0 + left
		iy0 := y0 + top
		ix1 := x1 - right
		iy1 := y1 - bottom
		if ix1 <= ix0 || iy1 <= iy0 {
			return 0, 0, false
		}
		isFocused := (n.Pane == wm.focused)
		cx, cy, hc := n.Pane.Draw(ix0, iy0, ix1, iy1, isFocused, screen)
		if isFocused && hc {
			return cx, cy, true
		}
		return 0, 0, false
	}

	total := 0
	for _, s := range n.Sizes {
		total += s
	}
	if total <= 0 {
		return 0, 0, false
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
			cx, cy, hc := wm.distribute(child, x0, pos0+sp[0], x1, pos0+sp[1], regionW, regionH, screen)
			if hc {
				cursorX, cursorY, hasCursor = cx, cy, true
			}
		} else {
			cx, cy, hc := wm.distribute(child, pos0+sp[0], y0, pos0+sp[1], y1, regionW, regionH, screen)
			if hc {
				cursorX, cursorY, hasCursor = cx, cy, true
			}
		}
	}
	return cursorX, cursorY, hasCursor
}

// gapGeometry derives the per-pane inset and shared-edge overlap from a
// requested gutter width.
//
// Leaves inset their rectangle by inset on internal sides and sibling spans
// within one container overlap by overlap cells on their shared edge
// (seamSpans), so two adjacent panes leave exactly
//
//	gutter = 2*inset - overlap
//
// empty cells between their borders (see edgeInsets for the smaller margin used
// on screen-edge sides).
func gapGeometry(g int) (gap, inset, overlap int) {
	if g < 0 {
		g = 0
	}
	inset = (g + 1) / 2
	if inset < 1 {
		inset = 1
	}
	overlap = 2*inset - g
	return g, inset, overlap
}

// seamSpans computes each child's [start, end) span along a split axis given
// the available length and proportional sizes.
//
// Children are laid out proportionally (span = avail*size/total, the last child
// absorbing the rounding remainder). Each child after the first overlaps the
// previous one by overlap cells on the shared edge; combined with the inset
// applied in distribute, that overlap leaves exactly gap cells between the two
// borders (see gapGeometry). Spans are clamped to never start before the axis
// origin.
func (wm *WindowManager) seamSpans(avail int, sizes []int) [][2]int {
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
			start = cursor - wm.overlap
			if start < 0 {
				start = 0
			}
		}
		spans[i] = [2]int{start, end}
		cursor = end
	}
	return spans
}

// statusDark is the modern dark-theme accent palette for the status bar. The
// base bar background/foreground come from the configurable StatusStyle; these
// accents are layered on top for the badge, the active window, mode banners,
// errors and the graph health dot.
var (
	statusBadgeBg  = tcell.NewRGBColor(0x2d, 0x6c, 0xdf) // indigo badge
	statusBadgeFg  = tcell.NewRGBColor(0xff, 0xff, 0xff)
	statusActiveBg = tcell.NewRGBColor(0x3a, 0x41, 0x4e) // raised block
	statusActiveFg = tcell.NewRGBColor(0xf0, 0xf3, 0xf6)
	statusModeBg   = tcell.NewRGBColor(0xf5, 0xc2, 0x42) // amber
	statusModeFg   = tcell.NewRGBColor(0x16, 0x1b, 0x22)
	statusErrBg    = tcell.NewRGBColor(0xe0, 0x4f, 0x4f) // red
	statusErrFg    = tcell.NewRGBColor(0xff, 0xff, 0xff)
	statusOnFg     = tcell.NewRGBColor(0x3f, 0xb9, 0x50) // green
	statusOffFg    = tcell.NewRGBColor(0xe0, 0x4f, 0x4f) // red
	statusFg       = tcell.Color(252) | tcell.ColorValid
	statusBg       = tcell.Color(235) | tcell.ColorValid
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

// statusIcon is the workspace glyph shown in the left badge.
const statusIcon = "⬢"

// statusSeg is one colored block of the status bar.
type statusSeg struct {
	text string
	st   tcell.Style
}

// renderStatus draws the tmux-style status line: an icon badge followed by the
// window list as "[index:name]" tabs (the active one highlighted), and a clock
// on the right. The badge turns amber while a prefix/resize/copy mode is armed
// and red on error (the message is shown after the window list). The whole row
// is blanked first so stale glyphs never survive a resize. Must run on the main
// goroutine.
func (wm *WindowManager) renderStatus(screen tcell.Screen, maxX, maxY int, prefixActive, resizeMode, copyMode bool, errorMsg string, style StatusStyle) {
	style = style.resolve()
	if maxX <= 0 || maxY <= 0 {
		return
	}
	y := maxY - 1

	bar := tcell.StyleDefault.Foreground(style.Fg).Background(style.Bg)
	modeSt := tcell.StyleDefault.Foreground(statusModeFg).Background(statusModeBg).Bold(true)
	errSt := tcell.StyleDefault.Foreground(statusErrFg).Background(statusErrBg).Bold(true)
	activeSt := tcell.StyleDefault.Foreground(statusActiveFg).Background(statusActiveBg).Bold(true)
	badgeSt := tcell.StyleDefault.Foreground(statusBadgeFg).Background(statusBadgeBg).Bold(true)

	// The badge reflects the active mode: indigo when idle, amber while a
	// prefix/resize/copy mode is armed, red on error.
	badge := badgeSt
	switch {
	case errorMsg != "":
		badge = errSt
	case prefixActive || resizeMode || copyMode:
		badge = modeSt
	}

	left := []statusSeg{{" " + statusIcon + " ", badge}}
	for i, name := range wm.WindowNames() {
		seg := statusSeg{fmt.Sprintf("[%d:%s]", i+1, name), bar}
		if i == wm.CurrentWin {
			seg.st = activeSt
		}
		left = append(left, seg)
	}
	if errorMsg != "" {
		left = append(left, statusSeg{" " + errorMsg + " ", errSt})
	}

	// Right group: graph health (when configured) and the clock.
	var right []statusSeg
	if wm.statusExtra != "" {
		fg := statusOffFg
		if strings.HasSuffix(wm.statusExtra, ":on") {
			fg = statusOnFg
		}
		right = append(right, statusSeg{
			" " + wm.statusExtra + " ",
			tcell.StyleDefault.Foreground(fg).Background(style.Bg).Bold(true),
		})
	}
	right = append(right, statusSeg{
		" " + time.Now().Format("15:04") + " ",
		tcell.StyleDefault.Foreground(style.Fg).Background(style.Bg).Bold(true),
	})

	for x := 0; x < maxX; x++ {
		screen.SetContent(x, y, ' ', nil, bar)
	}

	rightW := 0
	for _, seg := range right {
		rightW += len([]rune(seg.text))
	}
	rightStart := maxX - rightW
	drawRight := rightStart >= 1
	if !drawRight {
		rightStart = maxX
	}

	x := 0
	for _, seg := range left {
		for _, r := range []rune(seg.text) {
			if x >= rightStart {
				break
			}
			screen.SetContent(x, y, r, nil, seg.st)
			x++
		}
		if x >= rightStart {
			break
		}
	}

	if drawRight {
		x := rightStart
		for _, seg := range right {
			for _, r := range []rune(seg.text) {
				if x >= maxX {
					break
				}
				screen.SetContent(x, y, r, nil, seg.st)
				x++
			}
		}
	}
}
