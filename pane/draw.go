// Rendering a pane's border and emulator grid into the tcell screen buffer.
// These functions must only be called from the main goroutine.
package pane

import (
	"fmt"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
)

// Draw renders the border and terminal cells onto the screen within
// [x0, y0, x1, y1). Returns the absolute cursor coordinates and whether the
// cursor is visible.
//
// Per frame it: (1) records the rectangle and, if the inner size changed,
// resizes the emulator and pushes the new size to the shell via
// pty.Setsize so SIGWINCH reaches it; (2) paints the border (rounded corners,
// top/bottom, left/right) in the focus color, plus a "exited (...)" stub when
// the shell died; (3) copies the emulator grid into the screen buffer, skipping
// the continuation cells of double-width runes; (4) reports the logical cursor
// position (only meaningful when focused, and only when DECTCEM says it is
// visible). Run only from the main goroutine.
func (p *Pane) Draw(x0, y0, x1, y1 int, isFocused bool, screen tcell.Screen) (cx, cy int, hasCursor bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.x0, p.y0, p.x1, p.y1 = x0, y0, x1, y1

	w := x1 - x0 - 2
	if w < 1 {
		w = 1
	}
	h := y1 - y0 - 2
	if h < 1 {
		h = 1
	}

	if w != p.width || h != p.height {
		p.width = w
		p.height = h
		p.term.resize(w, h)
		p.ptyMtx.Lock()
		if p.pty != nil {
			if err := pty.Setsize(p.pty, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}); err != nil {
				p.log().Error("pty resize failed", "cols", w, "rows", h, "err", err)
			}
		}
		p.ptyMtx.Unlock()
	}

	borderStyle := tcell.StyleDefault.Foreground(colBorder)
	if isFocused {
		borderStyle = tcell.StyleDefault.Foreground(colBorderFocus).Bold(true)
	}

	// Draw corners
	screen.SetContent(x0, y0, '┌', nil, borderStyle)
	screen.SetContent(x1-1, y0, '┐', nil, borderStyle)
	screen.SetContent(x0, y1-1, '└', nil, borderStyle)
	screen.SetContent(x1-1, y1-1, '┘', nil, borderStyle)

	// Top and bottom borders
	for x := x0 + 1; x < x1-1; x++ {
		screen.SetContent(x, y0, '─', nil, borderStyle)
		screen.SetContent(x, y1-1, '─', nil, borderStyle)
	}

	// Left and right borders
	for y := y0 + 1; y < y1-1; y++ {
		screen.SetContent(x0, y, '│', nil, borderStyle)
		screen.SetContent(x1-1, y, '│', nil, borderStyle)
	}

	// Show window title in the top border when available. p.mu is already held
	// (we are inside Draw), so read the title directly from p.term instead of
	// calling the locking accessor Pane.Title to avoid a self-deadlock.
	if title := p.term.Title(); title != "" && len(title)+4 < x1-x0-2 {
		titleRunes := []rune(title)
		maxTitle := x1 - x0 - 4
		if len(titleRunes) > maxTitle {
			titleRunes = titleRunes[:maxTitle]
		}
		titleStyle := borderStyle
		if !isFocused {
			titleStyle = tcell.StyleDefault.Foreground(tcell.ColorDefault)
		}
		titleStart := x0 + 2
		for i, r := range titleRunes {
			screen.SetContent(titleStart+i, y0, r, nil, titleStyle)
		}
	}

	if p.dead {
		title := " dead "
		if p.exitMsg != "" {
			title = fmt.Sprintf(" %s ", p.exitMsg)
		}
		deadStyle := tcell.StyleDefault.Foreground(tcell.PaletteColor(1)).Bold(true)
		for i, r := range []rune(title) {
			if x0+2+i < x1-1 {
				screen.SetContent(x0+2+i, y0, r, nil, deadStyle)
			}
		}
	}

	innerX := x0 + 1
	innerY := y0 + 1

	// In copy mode the grid may be scrolled back; if so, render the view rows
	// (scrollback window above the live grid) and hide the cursor.
	viewRows := p.term.scrollOff
	for y := 0; y < h && y < p.term.rows+viewRows; y++ {
		row := p.term.viewRow(y)
		for x := 0; x < w && x < p.term.cols; x++ {
			c := row[x]
			if c.wide {
				continue // right half of a double-width rune drawn at x-1
			}
			ch := c.ch
			if ch == 0 {
				ch = ' '
			}
			screen.SetContent(innerX+x, innerY+y, ch, nil, cellToStyle(c))
		}
	}

	if isFocused && viewRows == 0 && p.term.cursorVisible {
		cx = innerX + p.term.x
		cy = innerY + p.term.y
		if cx >= innerX && cx < innerX+w && cy >= innerY && cy < innerY+h {
			hasCursor = true
		}
	}

	return cx, cy, hasCursor
}

// cellToStyle maps an emulator cell to a tcell style. Reverse video swaps
// fg/bg, default colors map to ColorDefault, palette cells carry their index,
// and truecolor cells carry their original RGB components; bold and underline
// become style attributes.
//
// The vtColor->tcell.Color translation mirrors the inverse of the emulator's
// SGR parsing: default stays default (so the surrounding terminal theme wins),
// palette indices map through tcell.PaletteColor, and RGB triples map through
// tcell.NewRGBColor.
func cellToStyle(c vtCell) tcell.Style {
	if c.reverse {
		c.fg, c.bg = c.bg, c.fg
	}
	st := tcell.StyleDefault
	if c.fg.def {
		st = st.Foreground(tcell.ColorDefault)
	} else if c.fg.rgb {
		st = st.Foreground(tcell.NewRGBColor(int32(c.fg.r), int32(c.fg.g), int32(c.fg.b)))
	} else {
		st = st.Foreground(tcell.PaletteColor(c.fg.idx))
	}
	if c.bg.def {
		st = st.Background(tcell.ColorDefault)
	} else if c.bg.rgb {
		st = st.Background(tcell.NewRGBColor(int32(c.bg.r), int32(c.bg.g), int32(c.bg.b)))
	} else {
		st = st.Background(tcell.PaletteColor(c.bg.idx))
	}
	if c.bold {
		st = st.Bold(true)
	}
	if c.dim {
		st = st.Dim(true)
	}
	if c.italic {
		st = st.Italic(true)
	}
	if c.underline {
		st = st.Underline(true)
	}
	if c.strikethrough {
		st = st.StrikeThrough(true)
	}
	return st
}
