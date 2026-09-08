// Package pane emulates a terminal per pane.
//
// terminal is a small VT100/xterm-compatible screen emulator. It exists
// because the UI only draws cells: every control sequence (cursor
// addressing, erase, scroll regions, alternate screen, ...) would otherwise
// leak into the pane as garbage text, and fullscreen programs (vim, htop,
// less) could never work. The emulator keeps the authoritative screen state;
// each Pane.Draw pass copies its grid into the screen buffer.
package pane

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// vtColor is a cell color: either the terminal default, an xterm palette
// index, or an explicit 24-bit RGB triple (truecolor SGR 38;2/48;2).
type vtColor struct {
	def bool
	rgb bool
	r   int
	g   int
	b   int
	idx int // palette index 0-255
}

// colorDefault returns the "unset/terminal default" color.
func colorDefault() vtColor { return vtColor{def: true} }

// colorIndex returns a palette color (xterm 256, index 0-255).
func colorIndex(n int) vtColor { return vtColor{idx: n} }

// colorRGB returns a truecolor value (SGR 38;2 / 48;2).
func colorRGB(r, g, b int) vtColor { return vtColor{rgb: true, r: r, g: g, b: b} }

// vtCell is one screen cell.
type vtCell struct {
	ch        rune
	fg, bg    vtColor
	bold      bool
	underline bool
	reverse   bool
	wide      bool // continuation cell of a double-width rune to the left
}

// blankCell returns the default empty cell (space, default colors).
func blankCell() vtCell { return vtCell{ch: ' ', fg: colorDefault(), bg: colorDefault()} }

// blankLine returns a row of blank cells of the given width.
func blankLine(cols int) []vtCell {
	l := make([]vtCell, cols)
	for i := range l {
		l[i] = blankCell()
	}
	return l
}

// terminal holds the full state of one emulated screen.
type terminal struct {
	cols, rows int
	grid       []vtCell // rows*cols, the visible screen (main or alt below)
	alt        []vtCell // alternate screen buffer, nil when unused
	altOn      bool
	x, y       int // cursor in the active buffer
	sx, sy     int // saved cursor (ESC 7 / CSI s) in the active buffer
	msx, msy   int // main-buffer cursor stashed while alt screen is active

	fg, bg               vtColor
	bold, underline, rev bool

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

	scrollback  [][]vtCell // main-buffer lines scrolled off the top
	scrollCap   int
	lastGraphic rune // for CSI REP (b)
	pending     []byte
	respond     func([]byte) // sends bytes back to the PTY (CPR/DA replies)
}

// newTerminal allocates a cols×rows screen in the default state, with respond
// invoked whenever the emulator must send bytes back to the shell (DA/CPR).
func newTerminal(cols, rows int, respond func([]byte)) *terminal {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	t := &terminal{
		cols:          cols,
		rows:          rows,
		grid:          make([]vtCell, cols*rows),
		fg:            colorDefault(),
		bg:            colorDefault(),
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
func (t *terminal) resetTabs() {
	t.tabs = map[int]bool{}
	for x := 8; x < t.cols; x += 8 {
		t.tabs[x] = true
	}
}

// at returns the active-grid cell at (x, y).
func (t *terminal) at(x, y int) *vtCell { return &t.grid[y*t.cols+x] }

// resize keeps the top-left content, clamps the cursor and resets the scroll
// region. The alternate buffer is dropped (apps redraw on SIGWINCH anyway).
func (t *terminal) resize(cols, rows int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols == t.cols && rows == t.rows {
		return
	}
	ng := make([]vtCell, cols*rows)
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
func (t *terminal) reset() {
	for i := range t.grid {
		t.grid[i] = blankCell()
	}
	t.alt, t.altOn = nil, false
	t.x, t.y, t.sx, t.sy, t.msx, t.msy = 0, 0, 0, 0, 0, 0
	t.fg, t.bg = colorDefault(), colorDefault()
	t.bold, t.underline, t.rev = false, false, false
	t.top, t.bot = 0, t.rows-1
	t.wrap, t.decckm, t.cursorVisible = true, false, true
	t.bracketedPaste = false
	t.g0, t.g1, t.gShifted = 0, 0, false
	t.resetTabs()
}

// ---------------------------------------------------------------- feeding ---

// process feeds raw PTY bytes into the emulator, buffering a truncated tail
// for the next read.
func (t *terminal) process(chunk []byte) {
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
func (t *terminal) control(b byte) {
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
func (t *terminal) put(r rune) {
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
	*c = vtCell{ch: r, fg: t.fg, bg: t.bg, bold: t.bold, underline: t.underline, reverse: t.rev}
	t.x++
}

// putWide writes a double-width (East Asian) rune at the cursor, marking the
// cell to its right as a continuation so Draw can skip it.
func (t *terminal) putWide(r rune) {
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
	*c = vtCell{ch: r, fg: t.fg, bg: t.bg, bold: t.bold, underline: t.underline, reverse: t.rev}
	if t.x+1 < t.cols {
		*t.at(t.x+1, t.y) = vtCell{ch: ' ', wide: true}
	}
	t.x = min(t.x+2, t.cols-1)
}

// linefeed moves down, scrolling the region when at its bottom.
func (t *terminal) linefeed() {
	if t.y == t.bot {
		t.scrollUp(1)
	} else if t.y < t.rows-1 {
		t.y++
	} else {
		t.scrollUp(1)
	}
}

// reverseIndex is ESC M.
func (t *terminal) reverseIndex() {
	if t.y == t.top {
		t.scrollDown(1)
	} else if t.y > 0 {
		t.y--
	}
}

// nextTab returns the next tab stop at or after the cursor, or the last column.
func (t *terminal) nextTab() int {
	for x := t.x + 1; x < t.cols; x++ {
		if t.tabs[x] {
			return x
		}
	}
	return t.cols - 1
}

// scrollUp moves region lines up; the top line enters scrollback when the
// region is the whole main screen.
func (t *terminal) scrollUp(n int) {
	if n < 1 {
		return
	}
	if n > t.bot-t.top+1 {
		n = t.bot - t.top + 1
	}
	fullMain := !t.altOn && t.top == 0 && t.bot == t.rows-1
	for k := 0; k < n; k++ {
		if fullMain {
			t.pushScrollback(t.gridRow(0))
		}
		copy(t.grid[t.top*t.cols:(t.bot+1)*t.cols-t.cols], t.grid[(t.top+1)*t.cols:(t.bot+1)*t.cols])
		t.clearRow(t.bot)
	}
}

// scrollDown moves region lines down (reverse scroll).
func (t *terminal) scrollDown(n int) {
	if n < 1 {
		return
	}
	if n > t.bot-t.top+1 {
		n = t.bot - t.top + 1
	}
	for k := 0; k < n; k++ {
		copy(t.grid[(t.top+1)*t.cols:(t.bot+1)*t.cols], t.grid[t.top*t.cols:(t.bot+1)*t.cols-t.cols])
		t.clearRow(t.top)
	}
}

// gridRow returns the backing slice for row y of the active grid.
func (t *terminal) gridRow(y int) []vtCell { return t.grid[y*t.cols : (y+1)*t.cols] }

// clearRow blanks every cell of row y.
func (t *terminal) clearRow(y int) {
	for x := 0; x < t.cols; x++ {
		*t.at(x, y) = blankCell()
	}
}

// pushScrollback copies a line into scrollback, trimming the buffer to the cap.
func (t *terminal) pushScrollback(row []vtCell) {
	cp := make([]vtCell, len(row))
	copy(cp, row)
	t.scrollback = append(t.scrollback, cp)
	if len(t.scrollback) > t.scrollCap {
		t.scrollback = t.scrollback[len(t.scrollback)-t.scrollCap:]
	}
}

// ------------------------------------------------------------- sequences ---

// parseEscape parses one escape sequence at buf[0] == ESC. Returns bytes
// consumed, or -1 when truncated (wait for more input).
func (t *terminal) parseEscape(buf []byte) int {
	if len(buf) < 2 {
		return -1
	}
	switch buf[1] {
	case '[':
		return t.parseCSI(buf)
	case ']':
		return consumeVTOSC(buf)
	case 'P', 'X', '^', '_':
		return consumeVTST(buf)
	case '(', ')':
		if len(buf) < 3 {
			return -1
		}
		t.setCharset(buf[1] == ')', buf[2])
		return 3
	case '7':
		t.sx, t.sy = t.x, t.y
		return 2
	case '8':
		t.x, t.y = t.clampX(t.sx), t.clampY(t.sy)
		return 2
	case 'M':
		t.reverseIndex()
		return 2
	case 'D':
		t.linefeed()
		return 2
	case 'E':
		t.x = 0
		t.linefeed()
		return 2
	case 'H':
		t.tabs[t.x] = true
		return 2
	case 'c':
		t.reset()
		return 2
	case 'N', 'O': // SS2/SS3 single shift: consume, next byte is literal.
		if len(buf) < 3 {
			return -1
		}
		return 2
	case '=', '>', '<', '#', '%', 'F', 'G', 'A', 'K', 'J', 'Z', 'n', 'o', '|', '}', '~', '\\':
		return 2
	default:
		return 2
	}
}

// setCharset programs G0 (g1=false) or G1 (g1=true) with the ASCII (B) or DEC
// special-graphics (0) mapping; SO/SI later select which set is active.
func (t *terminal) setCharset(g1 bool, b byte) {
	cs := 0
	if b == '0' {
		cs = 1
	}
	if g1 {
		t.g1 = cs
	} else {
		t.g0 = cs
	}
	// Active set follows SO/SI; most apps only program G0 then shift.
	_ = t.g0
	_ = t.g1
}

// clampX clamps a column index into the range [0, cols).
func (t *terminal) clampX(x int) int {
	if x < 0 {
		return 0
	}
	if x >= t.cols {
		return t.cols - 1
	}
	return x
}

// clampY clamps a row index into the range [0, rows).
func (t *terminal) clampY(y int) int {
	if y < 0 {
		return 0
	}
	if y >= t.rows {
		return t.rows - 1
	}
	return y
}

// parseCSI parses "ESC [ params intermediates final".
func (t *terminal) parseCSI(buf []byte) int {
	j := 2
	for j < len(buf) && buf[j] >= 0x30 && buf[j] <= 0x3F {
		j++
	}
	for j < len(buf) && buf[j] >= 0x20 && buf[j] <= 0x2F {
		j++
	}
	if j >= len(buf) {
		return -1
	}
	final := buf[j]
	params := string(buf[2:j])
	consumed := j + 1

	priv := ""
	if strings.HasPrefix(params, "?") || strings.HasPrefix(params, ">") ||
		strings.HasPrefix(params, "=") || strings.HasPrefix(params, "!") {
		priv, params = params[:1], params[1:]
	}
	nums := splitParams(params)

	switch final {
	case 'm':
		t.sgr(nums)
	case 'H', 'f':
		t.y = t.clampY(pn(nums, 0, 1) - 1)
		t.x = t.clampX(pn(nums, 1, 1) - 1)
	case 'A':
		t.y = t.clampY(t.y - pn(nums, 0, 1))
	case 'B', 'e':
		t.y = t.clampY(t.y + pn(nums, 0, 1))
	case 'C', 'a':
		t.x = t.clampX(t.x + pn(nums, 0, 1))
	case 'D':
		t.x = t.clampX(t.x - pn(nums, 0, 1))
	case 'E':
		t.y = t.clampY(t.y + pn(nums, 0, 1))
		t.x = 0
	case 'F':
		t.y = t.clampY(t.y - pn(nums, 0, 1))
		t.x = 0
	case 'G', '`':
		t.x = t.clampX(pn(nums, 0, 1) - 1)
	case 'd':
		t.y = t.clampY(pn(nums, 0, 1) - 1)
	case 'J':
		t.eraseDisplay(pn(nums, 0, 0))
	case 'K':
		t.eraseLine(pn(nums, 0, 0))
	case 'L':
		t.insertLines(pn(nums, 0, 1))
	case 'M':
		t.deleteLines(pn(nums, 0, 1))
	case 'P':
		t.deleteChars(pn(nums, 0, 1))
	case 'X':
		t.eraseChars(pn(nums, 0, 1))
	case '@':
		t.insertChars(pn(nums, 0, 1))
	case 'b':
		for i, n := 0, pn(nums, 0, 1); i < n; i++ {
			t.put(t.lastGraphic)
		}
	case 'S':
		t.scrollUp(pn(nums, 0, 1))
	case 'T':
		t.scrollDown(pn(nums, 0, 1))
	case 's':
		t.sx, t.sy = t.x, t.y
	case 'u':
		t.x, t.y = t.clampX(t.sx), t.clampY(t.sy)
	case 'r':
		if priv == "" {
			top, bot := pn(nums, 0, 1)-1, pn(nums, 1, t.rows)-1
			if top < 0 {
				top = 0
			}
			if bot >= t.rows {
				bot = t.rows - 1
			}
			if top < bot {
				t.top, t.bot = top, bot
				t.x, t.y = 0, top
			}
		}
	case 'g':
		if pn(nums, 0, 0) == 3 {
			t.resetTabs()
		} else {
			delete(t.tabs, t.x)
		}
	case 'c':
		t.deviceAttrs()
	case 'n':
		if pn(nums, 0, 0) == 6 {
			t.sendResponse([]byte("\x1b[" + strconv.Itoa(t.y+1) + ";" + strconv.Itoa(t.x+1) + "R"))
		}
	case 'h', 'l':
		t.setMode(priv, nums, final == 'h')
	default:
		// 't', 'q', 'p', 'i', 'o', 'z', '{', '}', '|', etc: ignore.
	}
	return consumed
}

// splitParams splits "1;38;5;2" into ints; empty/invalid fields become -1.
func splitParams(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			out = append(out, -1)
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			out = append(out, -1)
			continue
		}
		out = append(out, n)
	}
	return out
}

// pn returns the i-th param or def when missing/invalid.
func pn(nums []int, i, def int) int {
	if i < len(nums) && nums[i] >= 0 {
		return nums[i]
	}
	return def
}

// sgr applies Select Graphic Rendition attributes.
func (t *terminal) sgr(nums []int) {
	if len(nums) == 0 {
		t.fg, t.bg = colorDefault(), colorDefault()
		t.bold, t.underline, t.rev = false, false, false
		return
	}
	for i := 0; i < len(nums); i++ {
		n := nums[i]
		if n < 0 {
			n = 0
		}
		switch {
		case n == 0:
			t.fg, t.bg = colorDefault(), colorDefault()
			t.bold, t.underline, t.rev = false, false, false
		case n == 1:
			t.bold = true
		case n == 4:
			t.underline = true
		case n == 7:
			t.rev = true
		case n == 22:
			t.bold = false
		case n == 24:
			t.underline = false
		case n == 27:
			t.rev = false
		case n >= 30 && n <= 37:
			t.fg = colorIndex(n - 30)
		case n == 39:
			t.fg = colorDefault()
		case n >= 40 && n <= 47:
			t.bg = colorIndex(n - 40)
		case n == 49:
			t.bg = colorDefault()
		case n >= 90 && n <= 97:
			t.fg = colorIndex(n - 90 + 8)
		case n >= 100 && n <= 107:
			t.bg = colorIndex(n - 100 + 8)
		case n == 38 || n == 48:
			isFg := n == 38
			if i+1 >= len(nums) {
				break
			}
			switch nums[i+1] {
			case 5:
				if i+2 < len(nums) && nums[i+2] >= 0 && nums[i+2] <= 255 {
					if isFg {
						t.fg = colorIndex(nums[i+2])
					} else {
						t.bg = colorIndex(nums[i+2])
					}
					i += 2
				} else {
					i += 2
				}
			case 2:
				if i+4 < len(nums) {
					if isFg {
						t.fg = colorRGB(nums[i+2], nums[i+3], nums[i+4])
					} else {
						t.bg = colorRGB(nums[i+2], nums[i+3], nums[i+4])
					}
				}
				i += 4
			default:
				i++
			}
		default:
			// 2 (dim), 3 (italic), 5/6 (blink), 8/9, 28, 53, ...: ignore.
		}
	}
}

// setMode handles SM/RM (CSI h/l), including private modes.
func (t *terminal) setMode(priv string, nums []int, on bool) {
	if priv == "" {
		return // e.g. insert mode: ignore.
	}
	if priv != "?" {
		return
	}
	modes := nums
	if len(modes) == 0 {
		return
	}
	for _, m := range modes {
		switch m {
		case 25:
			t.cursorVisible = on
		case 1:
			t.decckm = on
		case 7:
			t.wrap = on
		case 47, 1047, 1049:
			if on {
				t.enterAlt()
			} else {
				t.exitAlt()
			}
		case 2004:
			t.bracketedPaste = on
		default:
			// 1000/1002/1006/1015/1016 (mouse), 1004 (focus), ...: ignore.
		}
	}
}

// enterAlt switches to the alternate screen, stashing the main buffer in t.alt
// along with the main-buffer cursor position.
func (t *terminal) enterAlt() {
	if t.altOn {
		return
	}
	t.msx, t.msy = t.x, t.y
	t.alt = make([]vtCell, t.cols*t.rows)
	for i := range t.alt {
		t.alt[i] = blankCell()
	}
	t.grid, t.alt = t.alt, t.grid
	t.altOn = true
	t.x, t.y, t.sx, t.sy = 0, 0, 0, 0
	t.top, t.bot = 0, t.rows-1
}

// exitAlt restores the main buffer. The main buffer was stashed in t.alt by
// the swap in enterAlt.
func (t *terminal) exitAlt() {
	if !t.altOn {
		return
	}
	t.grid, t.alt = t.alt, nil
	t.altOn = false
	t.x, t.y = t.clampX(t.msx), t.clampY(t.msy)
	t.top, t.bot = 0, t.rows-1
}

// deviceAttrs answers the device-attributes query (CSI c) as "VT100 with
// advanced video" so plain-line apps keep their ANSI behavior.
func (t *terminal) deviceAttrs() {
	t.sendResponse([]byte("\x1b[?6c"))
}

// sendResponse writes a reply (DA/CPR) back to the shell via the respond callback.
func (t *terminal) sendResponse(b []byte) {
	if t.respond != nil {
		t.respond(b)
	}
}

// ------------------------------------------------------------- editing -----

func (t *terminal) eraseDisplay(mode int) {
	switch mode {
	case 1: // cursor to start.
		for y := 0; y < t.y; y++ {
			t.clearRow(y)
		}
		for x := 0; x <= t.x && x < t.cols; x++ {
			*t.at(x, t.y) = blankCell()
		}
	case 2, 3: // whole screen (+scrollback for 3J).
		for y := 0; y < t.rows; y++ {
			t.clearRow(y)
		}
		if mode == 3 && !t.altOn {
			t.scrollback = nil
		}
	default: // 0: cursor to end.
		for x := t.x; x < t.cols; x++ {
			*t.at(x, t.y) = blankCell()
		}
		for y := t.y + 1; y < t.rows; y++ {
			t.clearRow(y)
		}
	}
}

// eraseLine clears the current line: 0 = cursor to end, 1 = start to cursor, 2 = whole line.
func (t *terminal) eraseLine(mode int) {
	switch mode {
	case 1:
		for x := 0; x <= t.x && x < t.cols; x++ {
			*t.at(x, t.y) = blankCell()
		}
	case 2:
		t.clearRow(t.y)
	default:
		for x := t.x; x < t.cols; x++ {
			*t.at(x, t.y) = blankCell()
		}
	}
}

// insertLines opens n blank lines at the cursor row within the scroll region.
func (t *terminal) insertLines(n int) {
	if t.y < t.top || t.y > t.bot || n < 1 {
		return
	}
	if n > t.bot-t.y+1 {
		n = t.bot - t.y + 1
	}
	for k := 0; k < n; k++ {
		copy(t.grid[(t.y+1)*t.cols:(t.bot+1)*t.cols], t.grid[t.y*t.cols:(t.bot+1)*t.cols-t.cols])
		for x := 0; x < t.cols; x++ {
			*t.at(x, t.y) = blankCell()
		}
	}
}

// deleteLines removes n lines at the cursor row, pulling lines below into the region.
func (t *terminal) deleteLines(n int) {
	if t.y < t.top || t.y > t.bot || n < 1 {
		return
	}
	if n > t.bot-t.y+1 {
		n = t.bot - t.y + 1
	}
	for k := 0; k < n; k++ {
		copy(t.grid[t.y*t.cols:(t.bot+1)*t.cols-t.cols], t.grid[(t.y+1)*t.cols:(t.bot+1)*t.cols])
		t.clearRow(t.bot)
	}
}

// insertChars shifts the cursor row right, blanking the cells vacated at the left.
func (t *terminal) insertChars(n int) {
	if n < 1 || t.x >= t.cols {
		return
	}
	if n > t.cols-t.x {
		n = t.cols - t.x
	}
	row := t.gridRow(t.y)
	copy(row[t.x+n:], row[t.x:t.cols-n])
	for x := t.x; x < t.x+n; x++ {
		row[x] = blankCell()
	}
}

// deleteChars shifts the cursor row left, blanking the cells vacated at the right.
func (t *terminal) deleteChars(n int) {
	if n < 1 || t.x >= t.cols {
		return
	}
	if n > t.cols-t.x {
		n = t.cols - t.x
	}
	row := t.gridRow(t.y)
	copy(row[t.x:], row[t.x+n:])
	for x := t.cols - n; x < t.cols; x++ {
		row[x] = blankCell()
	}
}

// eraseChars blanks n cells at and after the cursor without shifting the row.
func (t *terminal) eraseChars(n int) {
	if n < 1 {
		return
	}
	for k := 0; k < n && t.x+k < t.cols; k++ {
		*t.at(t.x+k, t.y) = blankCell()
	}
}

// ------------------------------------------------------------- reading -----

// lineText renders one stored line as plain text without trailing blanks.
func lineText(row []vtCell) string {
	end := len(row)
	for end > 0 && row[end-1].ch == ' ' {
		end--
	}
	var sb strings.Builder
	for _, c := range row[:end] {
		ch := c.ch
		if ch == 0 {
			ch = ' '
		}
		sb.WriteRune(ch)
	}
	return sb.String()
}

// allText returns scrollback + screen as plain text (for yank/copy).
func (t *terminal) allText() string {
	var sb strings.Builder
	for _, row := range t.scrollback {
		sb.WriteString(lineText(row))
		sb.WriteByte('\n')
	}
	grid := t.grid
	if t.altOn {
		grid = t.grid // alt has no scrollback; copy the visible screen.
	}
	for y := 0; y < t.rows; y++ {
		sb.WriteString(lineText(grid[y*t.cols : (y+1)*t.cols]))
		if y < t.rows-1 {
			sb.WriteByte('\n')
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// dump returns the visible grid as plain text (used by tests).
func (t *terminal) dump() string {
	var sb strings.Builder
	for y := 0; y < t.rows; y++ {
		sb.WriteString(lineText(t.gridRow(y)))
		if y < t.rows-1 {
			sb.WriteByte('\n')
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ------------------------------------------------------------- helpers -----

// consumeVTOSC consumes "ESC ] ... (BEL | ESC \)".
func consumeVTOSC(buf []byte) int {
	for i := 2; i < len(buf); i++ {
		if buf[i] == 0x07 {
			return i + 1
		}
		if buf[i] == 0x1b {
			if i+1 >= len(buf) {
				return -1
			}
			if buf[i+1] == '\\' {
				return i + 2
			}
		}
	}
	return -1
}

// consumeVTST consumes DCS/SOS/PM/APC up to ST or BEL.
func consumeVTST(buf []byte) int {
	for i := 2; i < len(buf); i++ {
		if buf[i] == 0x07 {
			return i + 1
		}
		if buf[i] == 0x1b {
			if i+1 >= len(buf) {
				return -1
			}
			if buf[i+1] == '\\' {
				return i + 2
			}
		}
	}
	return -1
}

// acsMap translates DEC special-graphics bytes to unicode (vim borders).
var acsMap = map[byte]rune{
	'_': ' ',
	'`': '◆',
	'a': '▒',
	'b': ' ',
	'c': ' ',
	'd': ' ',
	'e': ' ',
	'f': '°',
	'g': '±',
	'h': '█',
	'i': ' ',
	'j': '┘',
	'k': '┐',
	'l': '┌',
	'm': '└',
	'n': '┼',
	'o': '⎺',
	'p': '⎻',
	'q': '─',
	'r': '⎼',
	's': '⎽',
	't': '├',
	'u': '┤',
	'v': '┴',
	'w': '┬',
	'x': '│',
	'y': '≤',
	'z': '≥',
	'{': 'π',
	'|': '≠',
	'}': '£',
	'~': '·',
}
