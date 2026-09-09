// Package term provides a small, pure VT100/xterm-compatible screen emulator.
//
// It is used by the pane package to decode a shell's byte stream into a cell
// grid that the UI draws: every control sequence (cursor addressing, erase,
// scroll regions, alternate screen, SGR colors, ...) is interpreted here so
// fullscreen programs (vim, htop, less) work. The emulator keeps the
// authoritative screen state; each draw pass copies its grid into the tcell
// screen buffer.
//
// The emulator is deliberately minimal: it implements the sequences a modern
// shell and TUI need and ignores everything else. It holds no UI or PTY
// references, so it is testable in isolation.
package term

import (
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Color is a cell color: either the terminal default, an xterm palette index,
// or an explicit 24-bit RGB triple (truecolor SGR 38;2/48;2).
//
// Exactly one of Def / RGB / Idx is meaningful: "Def" wins when set, otherwise
// "RGB" when set, otherwise "Idx" carries the 0-255 palette index.
type Color struct {
	Def bool
	RGB bool
	R   int
	G   int
	B   int
	Idx int // palette index 0-255
}

// ColorDefault returns the "unset/terminal default" color.
//
// Used whenever a cell inherits the terminal's own fg/bg (e.g. after SGR 0,
// or for the initial blank screen) so the surrounding theme shows through.
func ColorDefault() Color { return Color{Def: true} }

// colorIndex returns a palette color (xterm 256, index 0-255).
//
// Produced by SGR 38;5;N / 48;5;N and the classic 30-37/40-47 range.
func colorIndex(n int) Color { return Color{Idx: n} }

// colorRGB returns a truecolor value (SGR 38;2 / 48;2).
//
// Keeps the original components (0-255 each) instead of quantizing to the
// nearest xterm 256 color, so 24-bit color passes through unchanged.
func colorRGB(r, g, b int) Color { return Color{RGB: true, R: r, G: g, B: b} }

// Cell is one screen cell.
type Cell struct {
	Ch            rune
	Fg, Bg        Color
	Bold          bool
	Dim           bool
	Italic        bool
	Underline     bool
	Reverse       bool
	Strikethrough bool
	Wide          bool // continuation cell of a double-width rune to the left
}

// blankCell returns the default empty cell (space, default colors).
func blankCell() Cell { return Cell{Ch: ' ', Fg: ColorDefault(), Bg: ColorDefault()} }

// blankLine returns a row of blank cells of the given width.
//
// Used to seed new rows when growing the grid on resize and to build fresh
// scrollback/alternate buffers.
func blankLine(cols int) []Cell {
	l := make([]Cell, cols)
	for i := range l {
		l[i] = blankCell()
	}
	return l
}

// Terminal holds the full state of one emulated screen.
type Terminal struct {
	cols, rows int
	grid       []Cell // rows*cols, the visible screen (main or alt below)
	alt        []Cell // alternate screen buffer, nil when unused
	altOn      bool
	x, y       int // cursor in the active buffer
	sx, sy     int // saved cursor (ESC 7 / CSI s) in the active buffer
	msx, msy   int // main-buffer cursor stashed while alt screen is active

	fg, bg            Color
	bold, dim, italic bool
	underline, rev    bool
	strikethrough     bool

	top, bot int // scroll region, 0-based inclusive
	wrap     bool
	decckm   bool // application cursor-keys mode (arrows send SS3)
	// cursorVisible tracks DECTCEM (CSI ?25 h/l); when hidden the cursor is
	// not drawn even in a focused pane.
	cursorVisible bool
	// bracketedPaste tracks DECSET 2004 (CSI ?2004 h/l): when the shell
	// enables it, pasted input must be wrapped in ESC[200~ / ESC[201~.
	bracketedPaste bool

	g0, g1   int  // charsets: 0 = ASCII (B), 1 = DEC special (0)
	gShifted bool // set by SO, cleared by SI

	tabs map[int]bool

	scrollback  [][]Cell // main-buffer lines scrolled off the top
	scrollCap   int
	scrollOff   int  // lines of scrollback viewable above the screen (copy mode)
	lastGraphic rune // for CSI REP (b)
	pending     []byte
	respond     func([]byte) // sends bytes back to the PTY (CPR/DA replies)
	title       string       // window title set via OSC 0/2
}

// New allocates a cols×rows screen in the default state, with respond invoked
// whenever the emulator must send bytes back to the shell (DA/CPR).
//
// The initial state is: blank grid, cursor at (0,0) and visible, wrap enabled,
// scroll region covering the whole screen, default colors, G0/G1 = ASCII with
// G0 shifted in, tab stops every 8 columns, and bracketed paste off.
func New(cols, rows int, respond func([]byte)) *Terminal {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	t := &Terminal{
		cols:          cols,
		rows:          rows,
		grid:          make([]Cell, cols*rows),
		fg:            ColorDefault(),
		bg:            ColorDefault(),
		top:           0,
		bot:           rows - 1,
		wrap:          true,
		cursorVisible: true,
		tabs:          map[int]bool{},
		scrollCap:     2000,
		respond:       respond,
	}
	for i := range t.grid {
		t.grid[i] = blankCell()
	}
	t.resetTabs()
	return t
}

// resetTabs clears the tab-stop map and sets one stop every 8 columns.
//
// Tab stops are ANSI-style (like a real terminal booting); reset by CSI g 3.
func (t *Terminal) resetTabs() {
	t.tabs = map[int]bool{}
	for x := 8; x < t.cols; x += 8 {
		t.tabs[x] = true
	}
}

// at returns the active-grid cell at (x, y).
//
// The active grid is t.grid whether or not the alternate screen is in use
// (enterAlt swaps buffers into t.grid). Callers must already hold the pane's
// mutex or be running single-threaded tests.
func (t *Terminal) at(x, y int) *Cell { return &t.grid[y*t.cols+x] }

// resize keeps the top-left content, clamps the cursor and resets the scroll
// region. The alternate buffer is dropped (apps redraw on SIGWINCH anyway).
//
// Growing the grid pads new rows/columns with blanks; shrinking it clips the
// right/bottom. This matches what a real terminal does for columns, and
// matches app expectations for rows since SIGWINCH-driven redraws are the norm.
func (t *Terminal) resize(cols, rows int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols == t.cols && rows == t.rows {
		return
	}
	ng := make([]Cell, cols*rows)
	for i := range ng {
		ng[i] = blankCell()
	}
	mc := min(cols, t.cols)
	mr := min(rows, t.rows)
	for y := 0; y < mr; y++ {
		copy(ng[y*cols:y*cols+mc], t.grid[y*t.cols:y*t.cols+mc])
	}
	t.grid, t.cols, t.rows = ng, cols, rows
	t.alt, t.altOn = nil, false
	t.top, t.bot = 0, rows-1
	t.x = min(t.x, cols-1)
	t.y = min(t.y, rows-1)
	t.sx, t.sy, t.msx, t.msy = 0, 0, 0, 0
	t.resetTabs()
}

// reset restores the initial state (RIS). Scrollback is preserved.
//
// This is the emulator-side of ESC c: it blanks the grid, drops the alternate
// screen, resets color/attributes, cursor, wrap, charsets and tab stops, and
// restores default visibility/modes, but keeps previously scrolled lines so
// yank/copy content survives a shell reset.
func (t *Terminal) reset() {
	for i := range t.grid {
		t.grid[i] = blankCell()
	}
	t.alt, t.altOn = nil, false
	t.x, t.y, t.sx, t.sy, t.msx, t.msy = 0, 0, 0, 0, 0, 0
	t.fg, t.bg = ColorDefault(), ColorDefault()
	t.bold, t.dim, t.italic, t.underline, t.rev, t.strikethrough = false, false, false, false, false, false
	t.top, t.bot = 0, t.rows-1
	t.wrap, t.decckm, t.cursorVisible = true, false, true
	t.bracketedPaste = false
	t.title = ""
	t.g0, t.g1, t.gShifted = 0, 0, false
	t.resetTabs()
}

// ---------------------------------------------------------------- feeding ---

// process feeds raw PTY bytes into the emulator, buffering a truncated tail
// for the next read.
//
// The byte stream is parsed inline: ESC starts a sequence (parseEscape may
// return -1 when the read ends mid-sequence, in which case the remainder is
// stashed in t.pending and prepended to the next chunk), C0 bytes below 0x20
// go through control, DEL is ignored, and everything else is decoded as UTF-8
// (buffering incomplete trailing runes) and written via put. G1 shifted-in
// characters route through the DEC special-graphics map.
func (t *Terminal) process(chunk []byte) {
	var buf []byte
	if len(t.pending) > 0 {
		buf = make([]byte, 0, len(t.pending)+len(chunk))
		buf = append(buf, t.pending...)
		buf = append(buf, chunk...)
		t.pending = nil
	} else {
		buf = chunk
	}
	i := 0
	for i < len(buf) {
		b := buf[i]
		if b == 0x1b {
			n := t.parseEscape(buf[i:])
			if n < 0 {
				t.pending = append(t.pending[:0], buf[i:]...)
				break
			}
			i += n
			continue
		}
		if b < 0x20 {
			t.control(b)
			i++
			continue
		}
		if b == 0x7f {
			i++ // DEL: ignore.
			continue
		}
		if !utf8.FullRune(buf[i:]) {
			t.pending = append(t.pending[:0], buf[i:]...)
			break
		}
		r, size := utf8.DecodeRune(buf[i:])
		if r == utf8.RuneError && size <= 1 {
			i++ // invalid byte: skip.
			continue
		}
		if size == 1 && t.gShifted && r < 0x80 {
			if m, ok := acsMap[byte(r)]; ok {
				r = m
			}
		}
		t.put(r)
		t.lastGraphic = r
		i += size
	}
}

// control handles C0 control bytes.
//
// Covers the controls programs actually emit: LF/VT/FF move down (respecting
// the scroll region), CR returns to column 0, TAB advances to the next stop,
// BS steps back one column (clamped to 0), and SO/SI swap between the G1 and
// G0 charsets. All others are ignored.
func (t *Terminal) control(b byte) {
	switch b {
	case '\n', '\v', '\f':
		t.linefeed()
	case '\r':
		t.x = 0
	case '\t':
		t.x = t.nextTab()
	case '\b':
		if t.x > 0 {
			t.x--
		}
	case '\x0e': // SO: use G1.
		t.gShifted = true
	case '\x0f': // SI: use G0.
		t.gShifted = false
	default: // NUL, BEL, XON/XOFF, etc: ignore.
	}
}

// put writes a graphic rune at the cursor and advances it.
//
// Handles wrap: when the cursor sits at the last column and wrap is enabled
// (default / DECSET 7), the rune is placed at the start of the next line
// (scrolling if needed); otherwise it overwrites the last column. Combining
// marks (runewidth 0) are dropped rather than shifting the row, and
// double-width runes are delegated to putWide. The cell inherits the current
// SGR attrs and fg/bg.
func (t *Terminal) put(r rune) {
	if t.y < 0 {
		t.y = 0
	}
	if t.y >= t.rows {
		t.y = t.rows - 1
	}
	if t.x < 0 {
		t.x = 0
	}
	if t.x >= t.cols {
		if t.wrap {
			t.x = 0
			t.linefeed()
		} else {
			t.x = t.cols - 1
		}
	}
	w := runewidth.RuneWidth(r)
	if w == 0 {
		// Combining mark: cells hold a single rune, so the mark is dropped
		// instead of shifting the row.
		return
	}
	if w > 1 {
		t.putWide(r)
		return
	}
	c := t.at(t.x, t.y)
	*c = Cell{Ch: r, Fg: t.fg, Bg: t.bg, Bold: t.bold, Dim: t.dim, Italic: t.italic, Underline: t.underline, Reverse: t.rev, Strikethrough: t.strikethrough}
	t.x++
}

// putWide writes a double-width (East Asian) rune at the cursor, marking the
// cell to its right as a continuation so Draw can skip it.
//
// If the rune would not fit (cursor at the last column) it wraps to the next
// line (or backs up one column when wrap is off). The continuation cell stores
// a space with wide=true; Draw renders the lead cell and jumps over the
// continuation.
func (t *Terminal) putWide(r rune) {
	if t.cols < 2 {
		t.put(r)
		return
	}
	if t.x >= t.cols-1 {
		// At the last column a wide rune cannot fit; wrap or clamp one left.
		if t.wrap {
			t.x = 0
			t.linefeed()
		} else {
			t.x = t.cols - 2
		}
	}
	c := t.at(t.x, t.y)
	*c = Cell{Ch: r, Fg: t.fg, Bg: t.bg, Bold: t.bold, Dim: t.dim, Italic: t.italic, Underline: t.underline, Reverse: t.rev, Strikethrough: t.strikethrough}
	if t.x+1 < t.cols {
		*t.at(t.x+1, t.y) = Cell{Ch: ' ', Wide: true}
	}
	t.x = min(t.x+2, t.cols-1)
}

// linefeed moves down, scrolling the region when at its bottom.
//
// When the cursor is on the bottom line of the scroll region the region is
// scrolled up one row (the top line enters scrollback for a full-screen main
// buffer); otherwise the cursor simply advances.
func (t *Terminal) linefeed() {
	if t.y == t.bot {
		t.scrollUp(1)
	} else if t.y < t.rows-1 {
		t.y++
	} else {
		t.scrollUp(1)
	}
}

// reverseIndex is ESC M: move the cursor up one line, scrolling the region
// down when the cursor is on its top line.
func (t *Terminal) reverseIndex() {
	if t.y == t.top {
		t.scrollDown(1)
	} else if t.y > 0 {
		t.y--
	}
}

// nextTab returns the next tab stop at or after the cursor, or the last column.
func (t *Terminal) nextTab() int {
	for x := t.x + 1; x < t.cols; x++ {
		if t.tabs[x] {
			return x
		}
	}
	return t.cols - 1
}

// gridRow returns the backing slice for row y of the active grid.
func (t *Terminal) gridRow(y int) []Cell { return t.grid[y*t.cols : (y+1)*t.cols] }

// clearRow blanks every cell of row y.
func (t *Terminal) clearRow(y int) {
	for x := 0; x < t.cols; x++ {
		*t.at(x, y) = blankCell()
	}
}

// ---------------------------------------------------- exported accessors ---

// Rows returns the number of screen rows.
func (t *Terminal) Rows() int { return t.rows }

// Cols returns the number of screen columns.
func (t *Terminal) Cols() int { return t.cols }

// Cell returns the live-grid cell at (x, y).
func (t *Terminal) Cell(x, y int) *Cell { return t.at(x, y) }

// ViewRow returns the cell row visible at view row yy (see viewRow).
func (t *Terminal) ViewRow(yy int) []Cell { return t.viewRow(yy) }

// CursorVisible reports whether DECTCEM says the cursor should be drawn.
func (t *Terminal) CursorVisible() bool { return t.cursorVisible }

// CursorX returns the cursor column.
func (t *Terminal) CursorX() int { return t.x }

// CursorY returns the cursor row.
func (t *Terminal) CursorY() int { return t.y }

// AppKeys reports whether application cursor-key mode (DECCKM) is active.
func (t *Terminal) AppKeys() bool { return t.decckm }

// BracketedPaste reports whether DECSET 2004 bracketed paste is enabled.
func (t *Terminal) BracketedPaste() bool { return t.bracketedPaste }

// Process feeds raw bytes into the emulator (public wrapper for process).
func (t *Terminal) Process(chunk []byte) { t.process(chunk) }

// Resize resizes the emulator (public wrapper for resize).
func (t *Terminal) Resize(cols, rows int) { t.resize(cols, rows) }

// Title returns the window title set by the shell via OSC 0/2.
func (t *Terminal) Title() string { return t.title }

// AllText returns scrollback + screen as plain text (for yank/copy).
func (t *Terminal) AllText() string { return t.allText() }
