// Package term provides the pane's screen emulator.
//
// It is a thin adapter over the charmbracelet/x/vt virtual terminal emulator,
// which does the actual VT100/xterm parsing (cursor addressing, erase, scroll
// regions, alternate screen, SGR colors, modern sequences such as kitty
// keyboard and OSC 52). This package translates the library's cell model into
// the small Color/Cell types the UI draws and keeps the copy-mode view and
// text-extraction helpers.
//
// The emulator holds no UI or PTY references, so it is testable in isolation.
// Like the library it wraps, it is not safe for concurrent use: callers must
// serialize Process/Resize against the draw-side accessors.
package term

import (
	"image/color"
	"io"
	"strings"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	ansi "github.com/charmbracelet/x/ansi"
	vt "github.com/charmbracelet/x/vt"
)

// scrollCap is the maximum number of scrollback lines retained.
const scrollCap = 2000

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
// Used whenever a cell inherits the terminal's own fg/bg (e.g. after SGR 0, or
// for the initial blank screen) so the surrounding theme shows through.
func ColorDefault() Color { return Color{Def: true} }

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

// Terminal holds one emulated screen: the wrapped library emulator plus the
// small amount of state the library does not expose (copy-mode offset, window
// title, and the two keyboard/paste modes the UI reads).
type Terminal struct {
	emu *vt.Emulator

	respond    func([]byte) // sends emulator replies (CPR/DA) back to the PTY
	scrollOff  int          // scrollback lines shown above the screen (copy mode)
	title      string       // window title set via OSC 0/2
	decckm     bool         // application cursor-keys mode (DECCKM, mode 1)
	bracketPas bool         // bracketed paste (DECSET 2004)
}

// New allocates a cols×rows screen and, when respond is non-nil, forwards any
// bytes the emulator sends back to the shell (DA/CPR replies) to it.
//
// The responder runs on a background goroutine: the library delivers replies
// through an io.Pipe, whose writes block until read, so the pipe must always be
// drained even when respond is nil.
func New(cols, rows int, respond func([]byte)) *Terminal {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	e := vt.NewEmulator(cols, rows)
	e.SetScrollbackSize(scrollCap)

	t := &Terminal{emu: e, respond: respond}
	e.SetCallbacks(vt.Callbacks{
		Title:       func(s string) { t.title = s },
		EnableMode:  func(m ansi.Mode) { t.setMode(m, true) },
		DisableMode: func(m ansi.Mode) { t.setMode(m, false) },
	})

	go t.drain()
	return t
}

// setMode tracks the DEC private modes the UI needs: application cursor keys
// (DECCKM) and bracketed paste (2004).
func (t *Terminal) setMode(m ansi.Mode, on bool) {
	switch m {
	case ansi.ModeCursorKeys:
		t.decckm = on
	case ansi.ModeBracketedPaste:
		t.bracketPas = on
	}
}

// drain reads emulator replies from the library's output pipe and forwards
// them to respond. It exits when Close is called.
func (t *Terminal) drain() {
	buf := make([]byte, 256)
	for {
		n, err := t.emu.Read(buf)
		if n > 0 && t.respond != nil {
			t.respond(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// Process feeds raw PTY bytes into the emulator.
func (t *Terminal) Process(chunk []byte) {
	_, _ = t.emu.Write(chunk)
}

// Resize changes the screen dimensions.
func (t *Terminal) Resize(cols, rows int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	t.emu.Resize(cols, rows)
}

// Close shuts down the emulator, unblocking the responder goroutine.
//
// It closes the emulator's input pipe rather than calling the library's Close,
// whose unsynchronized "closed" flag would race the responder's Read. It is
// safe to call after the last Process/Resize.
func (t *Terminal) Close() {
	switch w := t.emu.InputPipe().(type) {
	case interface{ CloseWithError(error) error }:
		_ = w.CloseWithError(io.EOF)
	case io.Closer:
		_ = w.Close()
	}
}

// ---------------------------------------------------- exported accessors ---

// Rows returns the number of screen rows.
func (t *Terminal) Rows() int { return t.emu.Height() }

// Cols returns the number of screen columns.
func (t *Terminal) Cols() int { return t.emu.Width() }

// Cell returns a copy of the live-grid cell at (x, y), or nil if out of bounds.
func (t *Terminal) Cell(x, y int) *Cell {
	if x < 0 || y < 0 || x >= t.emu.Width() || y >= t.emu.Height() {
		return nil
	}
	c := convCell(t.emu.CellAt(x, y))
	return &c
}

// ViewRow returns the cell row visible at view row yy (see viewRow).
//
// Rows from the scrollback fill the top of the view when the copy-mode offset
// is set; the live grid follows.
func (t *Terminal) ViewRow(yy int) []Cell {
	row := make([]Cell, t.emu.Width())
	if yy < t.scrollOff {
		idx := t.emu.ScrollbackLen() - t.scrollOff + yy
		if line := t.scrollbackLine(idx); line != nil {
			for x := range row {
				if x < len(line) {
					row[x] = convCell(&line[x])
				} else {
					row[x] = blankCell()
				}
			}
			return row
		}
	}
	gy := yy - t.scrollOff
	for x := range row {
		row[x] = convCell(t.emu.CellAt(x, gy))
	}
	return row
}

// scrollbackLine returns the scrollback line at index idx (0 = oldest), or nil.
func (t *Terminal) scrollbackLine(idx int) uv.Line {
	sb := t.emu.Scrollback()
	if sb == nil {
		return nil
	}
	return sb.Line(idx)
}

// CursorVisible reports whether DECTCEM says the cursor should be drawn.
func (t *Terminal) CursorVisible() bool { return !t.emu.CursorHidden() }

// CursorX returns the cursor column.
func (t *Terminal) CursorX() int { return t.emu.CursorPosition().X }

// CursorY returns the cursor row.
func (t *Terminal) CursorY() int { return t.emu.CursorPosition().Y }

// AppKeys reports whether application cursor-key mode (DECCKM) is active.
func (t *Terminal) AppKeys() bool { return t.decckm }

// BracketedPaste reports whether DECSET 2004 bracketed paste is enabled.
func (t *Terminal) BracketedPaste() bool { return t.bracketPas }

// AltScreen reports whether the alternate screen buffer is active.
func (t *Terminal) AltScreen() bool { return t.emu.IsAltScreen() }

// ScrollbackLen returns the number of scrollback lines retained.
func (t *Terminal) ScrollbackLen() int { return t.emu.ScrollbackLen() }

// Title returns the window title set by the shell via OSC 0/2.
func (t *Terminal) Title() string { return t.title }

// SetScrollOffset sets the copy-mode view offset: how many scrollback lines
// are shown above the screen. Clamped to [0, scrollback depth]. A zero offset
// shows the live screen.
func (t *Terminal) SetScrollOffset(n int) {
	if n < 0 {
		n = 0
	}
	if max := t.emu.ScrollbackLen(); n > max {
		n = max
	}
	t.scrollOff = n
}

// ScrollOffset returns the current copy-mode view offset.
func (t *Terminal) ScrollOffset() int { return t.scrollOff }

// AllText returns scrollback + screen as plain text (for yank/copy).
//
// Scrollback lines come first (each followed by a newline), then the visible
// grid rows. The alternate screen has no scrollback. A trailing newline is
// trimmed so the result is line-oriented.
func (t *Terminal) AllText() string {
	var b strings.Builder
	if !t.emu.IsAltScreen() {
		if sb := t.emu.Scrollback(); sb != nil {
			for _, line := range sb.Lines() {
				b.WriteString(lineTextFromUV(line))
				b.WriteByte('\n')
			}
		}
	}
	h := t.emu.Height()
	for y := 0; y < h; y++ {
		b.WriteString(lineText(t.screenRow(y)))
		if y < h-1 {
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// screenRow converts live-grid row y into the package's Cell type.
func (t *Terminal) screenRow(y int) []Cell {
	row := make([]Cell, t.emu.Width())
	for x := range row {
		row[x] = convCell(t.emu.CellAt(x, y))
	}
	return row
}

// ------------------------------------------------------- cell conversion ---

// convColor maps a library color to the package's Color type. A nil color is
// the terminal default; basic/palette colors keep their index; truecolor keeps
// its RGB components.
func convColor(c color.Color) Color {
	switch v := c.(type) {
	case nil:
		return ColorDefault()
	case ansi.BasicColor:
		return Color{Idx: int(v)}
	case ansi.IndexedColor:
		return Color{Idx: int(v)}
	case ansi.TrueColor:
		return Color{RGB: true, R: int(v>>16) & 0xff, G: int(v>>8) & 0xff, B: int(v) & 0xff}
	case ansi.RGBColor:
		return Color{RGB: true, R: int(v.R), G: int(v.G), B: int(v.B)}
	case color.RGBA:
		return Color{RGB: true, R: int(v.R), G: int(v.G), B: int(v.B)}
	default:
		r, g, bl, _ := c.RGBA()
		return Color{RGB: true, R: int(r >> 8), G: int(g >> 8), B: int(bl >> 8)}
	}
}

// convCell maps a library cell to the package's Cell type. A nil cell is a
// blank cell; a zero-width cell is the continuation of a wide rune to its left.
func convCell(c *uv.Cell) Cell {
	if c == nil {
		return blankCell()
	}
	out := Cell{
		Ch:            ' ',
		Fg:            convColor(c.Style.Fg),
		Bg:            convColor(c.Style.Bg),
		Bold:          c.Style.Attrs&uv.AttrBold != 0,
		Dim:           c.Style.Attrs&uv.AttrFaint != 0,
		Italic:        c.Style.Attrs&uv.AttrItalic != 0,
		Underline:     c.Style.Underline != uv.UnderlineStyleNone,
		Reverse:       c.Style.Attrs&uv.AttrReverse != 0,
		Strikethrough: c.Style.Attrs&uv.AttrStrikethrough != 0,
	}
	if c.Width == 0 {
		out.Wide = true
		return out
	}
	if c.Content != "" {
		if r, _ := utf8.DecodeRuneInString(c.Content); r != utf8.RuneError || len(c.Content) == 1 {
			out.Ch = r
		}
	}
	return out
}

// lineText renders one converted line as plain text without trailing blanks.
// NUL cells render as spaces so joined lines do not accumulate padding.
func lineText(row []Cell) string {
	end := len(row)
	for end > 0 && row[end-1].Ch == ' ' {
		end--
	}
	var sb strings.Builder
	for _, c := range row[:end] {
		ch := c.Ch
		if ch == 0 {
			ch = ' '
		}
		sb.WriteRune(ch)
	}
	return sb.String()
}

// lineTextFromUV converts a library line and renders it as plain text.
func lineTextFromUV(line uv.Line) string {
	row := make([]Cell, len(line))
	for x := range line {
		row[x] = convCell(&line[x])
	}
	return lineText(row)
}
