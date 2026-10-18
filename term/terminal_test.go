package term

import (
	"strings"
	"testing"
	"time"
)

// newTestTerm returns a ready-to-use 20x5 emulator in the default state, with
// no respond callback (the tests never trigger DA/CPR replies).
func newTestTerm() *Terminal {
	return New(20, 5, nil)
}

// feed feeds raw escape-sequence bytes into the emulator as one process chunk.
func feed(t *testing.T, term *Terminal, in string) {
	t.Helper()
	term.Process([]byte(in))
}

// dump renders the visible grid as plain text (trailing blank lines trimmed),
// matching how the copy/all-text helpers render lines.
func dump(t *Terminal) string {
	var rows []string
	for y := 0; y < t.Rows(); y++ {
		rows = append(rows, lineText(t.ViewRow(y)))
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}

// TestPutBasic checks that plain printable characters land in the grid at the
// cursor and advance it.
func TestPutBasic(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "hello")
	if got := dump(term); got != "hello" {
		t.Fatalf("dump = %q, want %q", got, "hello")
	}
}

// TestCursorMovement verifies CSI row/column addressing (H) and the relative
// up (A) / left (D) motions, including clamping at column 0.
func TestCursorMovement(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "\x1b[2;3H")
	feed(t, term, "XY")
	lines := strings.Split(dump(term), "\n")
	if len(lines) < 2 || lines[1] != "  XY" {
		t.Fatalf("row 2 = %q, want %q", lines[1], "  XY")
	}
	feed(t, term, "\x1b[1A\x1b[1D")
	if term.CursorX() != 3 || term.CursorY() != 0 {
		t.Fatalf("cursor = (%d,%d), want (3,0)", term.CursorX(), term.CursorY())
	}
	feed(t, term, "\x1b[3D")
	if term.CursorX() != 0 {
		t.Fatalf("cursor x = %d, want 0", term.CursorX())
	}
}

// TestEraseLine verifies EC (CSI K) erases from the cursor to the end of the
// line.
func TestEraseLine(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "abcdefgh")
	feed(t, term, "\x1b[1D\x1b[1D\x1b[K")
	if got := dump(term); got != "abcdef" {
		t.Fatalf("dump = %q, want %q", got, "abcdef")
	}
}

// TestEraseDisplay verifies ED (CSI 2J) blanks the whole screen.
func TestEraseDisplay(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "abc\r\ndef")
	feed(t, term, "\x1b[2J")
	if got := dump(term); got != "" {
		t.Fatalf("dump = %q, want empty", got)
	}
}

// TestAltScreen exercises the alternate-screen buffer (CSI ?1049 h/l): writing
// while the alt screen is active must not disturb the main buffer, and leaving
// it restores the main buffer and its content.
func TestAltScreen(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "main")
	feed(t, term, "\x1b[?1049h")
	if !term.AltScreen() {
		t.Fatal("alt screen not active")
	}
	feed(t, term, "ALT")
	if got := dump(term); got != "ALT" {
		t.Fatalf("alt dump = %q, want %q", got, "ALT")
	}
	feed(t, term, "\x1b[?1049l")
	if term.AltScreen() {
		t.Fatal("alt screen still active")
	}
	if got := dump(term); got != "main" {
		t.Fatalf("main dump = %q, want %q", got, "main")
	}
}

// TestACSGraphics verifies DEC special-graphics: programming G0 with charset 0
// (ESC (0) turns 'q' into a box-drawing horizontal line, and ESC (B returns to
// ASCII.
func TestACSGraphics(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "\x1b(0") // program G0 with DEC special graphics
	feed(t, term, "q")      // -> box drawing horizontal
	feed(t, term, "\x1b(B") // back to ASCII
	feed(t, term, "a")
	if got := dump(term); got != "\u2500a" {
		t.Fatalf("dump = %q, want %q", got, "\u2500a")
	}
}

// TestWideChars verifies a double-width CJK rune occupies two cells (the lead
// cell plus a marked continuation), advances the cursor by two, and does not
// corrupt the following narrow character.
func TestWideChars(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "\u4f60a") // CJK wide, then narrow
	if c := term.Cell(0, 0); c.Ch != '\u4f60' {
		t.Fatalf("cell0 = %q, want wide rune", c.Ch)
	}
	if !term.Cell(1, 0).Wide {
		t.Fatal("continuation cell not marked wide")
	}
	if got := term.Cell(2, 0).Ch; got != 'a' {
		t.Fatalf("cell2 = %q, want %q", got, 'a')
	}
	if term.CursorX() != 3 {
		t.Fatalf("cursor x = %d, want 3", term.CursorX())
	}
	if got := dump(term); got != "\u4f60 a" {
		t.Fatalf("dump = %q, want %q", got, "\u4f60 a")
	}
}

// TestWrapAtLastColumn checks the pending-wrap behaviour at the right margin:
// a character printed in the last column stays there, and the next character
// wraps to the start of the following line.
func TestWrapAtLastColumn(t *testing.T) {
	term := New(5, 3, nil)
	defer term.Close()
	feed(t, term, "\x1b[5G") // cursor to last column (x=4)
	feed(t, term, "a")
	feed(t, term, "b")
	if term.CursorY() != 1 || term.CursorX() != 1 {
		t.Fatalf("cursor = (%d,%d), want (1,1)", term.CursorX(), term.CursorY())
	}
	if got := term.Cell(4, 0).Ch; got != 'a' {
		t.Fatalf("cell(4,0) = %q, want 'a'", got)
	}
	if got := term.Cell(0, 1).Ch; got != 'b' {
		t.Fatalf("cell(0,1) = %q, want 'b'", got)
	}
}

// TestCombiningMarksSameCell verifies that a zero-width combining mark stays
// with its base rune (the library stores grapheme clusters) and does not
// advance the cursor.
func TestCombiningMarksSameCell(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "e\u0301")
	if got := term.Cell(0, 0).Ch; got != 'e' {
		t.Fatalf("cell = %q, want base rune %q", got, 'e')
	}
	if term.CursorX() != 1 {
		t.Fatalf("cursor x = %d, want 1", term.CursorX())
	}
}

// TestScrollbackAndAllText verifies that lines scrolled off the top of a
// full-screen main buffer enter scrollback and are included by AllText.
func TestScrollbackAndAllText(t *testing.T) {
	term := New(5, 2, nil)
	defer term.Close()
	feed(t, term, "123\r\n456\r\n7")
	if got := term.AllText(); got != "123\n456\n7" {
		t.Fatalf("AllText = %q, want %q", got, "123\n456\n7")
	}
}

// TestSGRColors verifies ANSI fg/bg attribute parsing: 38;5;196 selects a
// palette foreground, and SGR 0 resets the foreground to default.
func TestSGRColors(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "\x1b[38;5;196mX")
	if c := term.Cell(0, 0); c.Fg.Idx != 196 || c.Ch != 'X' {
		t.Fatalf("cell = %+v, want fg idx 196 ch X", c)
	}
	feed(t, term, "\x1b[0mY")
	if c := term.Cell(1, 0); !c.Fg.Def || c.Ch != 'Y' {
		t.Fatalf("cell = %+v, want default fg reset", c)
	}
}

// TestSGRTruecolor verifies 24-bit SGR parsing for both foreground (38;2;R;G;B)
// and background (48;2;R;G;B), with the original components preserved.
func TestSGRTruecolor(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "\x1b[38;2;255;0;128mX")
	c := term.Cell(0, 0)
	if !c.Fg.RGB || c.Fg.R != 255 || c.Fg.G != 0 || c.Fg.B != 128 {
		t.Fatalf("fg = %+v, want truecolor 255;0;128", c.Fg)
	}
	feed(t, term, "\x1b[48;2;1;2;3mY")
	c = term.Cell(1, 0)
	if !c.Bg.RGB || c.Bg.R != 1 || c.Bg.G != 2 || c.Bg.B != 3 {
		t.Fatalf("bg = %+v, want truecolor 1;2;3", c.Bg)
	}
}

// TestBracketedPasteMode verifies DECSET 2004 toggles the bracketed-paste flag
// that the UI uses to wrap pasted input.
func TestBracketedPasteMode(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	if term.BracketedPaste() {
		t.Fatal("bracketed paste should default to off")
	}
	feed(t, term, "\x1b[?2004h")
	if !term.BracketedPaste() {
		t.Fatal("bracketed paste should be on after CSI ?2004 h")
	}
	feed(t, term, "\x1b[?2004l")
	if term.BracketedPaste() {
		t.Fatal("bracketed paste should be off after CSI ?2004 l")
	}
}

// TestScrollRegion verifies that a restricted scroll region (CSI r) only
// accepts line feeds within it: rows above the region stay untouched and the
// bottom of the region is cleared when a line is scrolled in.
func TestScrollRegion(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "\x1b[2;4r")
	feed(t, term, "\x1b[2;1H")
	feed(t, term, "abc\r\ndef\r\nghi")
	feed(t, term, "\n") // scroll once inside region rows 2-4
	rows := strings.Split(dump(term), "\n")
	if len(rows) != 3 || rows[0] != "" || rows[1] != "def" || rows[2] != "ghi" {
		t.Fatalf("dump = %q, want rows (empty/def/ghi)", dump(term))
	}
	// Row above the region must not accept the scrolled line.
	if got := term.Cell(0, 0).Ch; got != ' ' {
		t.Fatalf("row 1 cell = %q, want space (outside region untouched)", got)
	}
	if got := term.Cell(0, 3).Ch; got != ' ' {
		t.Fatalf("bottom of region not cleared, got %q", got)
	}
}

// TestCursorVisibleMode verifies DECTCEM: CSI ?25 l hides the cursor and
// CSI ?25 h restores it.
func TestCursorVisibleMode(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	if !term.CursorVisible() {
		t.Fatal("cursor should default to visible")
	}
	feed(t, term, "\x1b[?25l")
	if term.CursorVisible() {
		t.Fatal("cursor should be hidden after CSI ?25 l")
	}
	feed(t, term, "\x1b[?25h")
	if !term.CursorVisible() {
		t.Fatal("cursor should be visible after CSI ?25 h")
	}
}

// TestOSCTitle verifies that OSC 0 and OSC 2 set the window title, and that
// both BEL and ST terminators are accepted. Other OSC codes are ignored.
func TestOSCTitle(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "\x1b]0;my title\x07")
	if got := term.Title(); got != "my title" {
		t.Fatalf("title = %q, want %q", got, "my title")
	}
	feed(t, term, "\x1b]2;another\x1b\\")
	if got := term.Title(); got != "another" {
		t.Fatalf("title = %q, want %q", got, "another")
	}
	// Non-title OSC (7 = cwd) must not clobber the title.
	feed(t, term, "\x1b]7;/home/user\x07")
	if got := term.Title(); got != "another" {
		t.Fatalf("title = %q, want %q", got, "another")
	}
}

// TestSGRDimItalicStrikethrough verifies SGR 2 (dim), 3 (italic) and 9
// (strikethrough) set the per-cell attributes and their resets clear them.
func TestSGRDimItalicStrikethrough(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	feed(t, term, "\x1b[2;3;9mX")
	c := term.Cell(0, 0)
	if !c.Dim || !c.Italic || !c.Strikethrough {
		t.Fatalf("cell = %+v, want dim+italic+strike", c)
	}
	feed(t, term, "\x1b[22;23;29mY")
	c = term.Cell(1, 0)
	if c.Dim || c.Italic || c.Strikethrough {
		t.Fatalf("cell = %+v, want attributes cleared", c)
	}
	// SGR 0 resets everything at once.
	feed(t, term, "\x1b[3;9mA")
	feed(t, term, "\x1b[0mB")
	c = term.Cell(3, 0)
	if c.Dim || c.Italic || c.Strikethrough || c.Bold {
		t.Fatalf("cell = %+v, want clean cell after SGR 0", c)
	}
}

// TestOSCTitlePartial verifies that an OSC spanning two reads (split mid-
// payload) is still parsed once the remainder arrives.
func TestOSCTitlePartial(t *testing.T) {
	term := newTestTerm()
	defer term.Close()
	term.Process([]byte("\x1b]0;my tit"))
	if term.Title() != "" {
		t.Fatal("title should be empty until the terminator arrives")
	}
	term.Process([]byte("le\x07"))
	if got := term.Title(); got != "my title" {
		t.Fatalf("title = %q, want %q", got, "my title")
	}
}

// TestRespondCPR verifies the emulator-to-PTY reply path: a cursor-position
// report (CSI 6n) is answered on the responder goroutine.
func TestRespondCPR(t *testing.T) {
	replies := make(chan string, 4)
	term := New(20, 5, func(b []byte) { replies <- string(b) })
	defer term.Close()
	feed(t, term, "\x1b[2;3H")
	feed(t, term, "\x1b[6n")
	select {
	case got := <-replies:
		if got != "\x1b[2;3R" {
			t.Fatalf("CPR reply = %q, want %q", got, "\x1b[2;3R")
		}
	case <-time.After(time.Second):
		t.Fatal("no CPR reply")
	}
}

// TestScrollbackView verifies copy-mode scrolling: SetScrollOffset makes the
// last N scrollback lines visible above the live grid through ViewRow, and the
// offset is clamped to the scrollback depth.
func TestScrollbackView(t *testing.T) {
	term := New(5, 2, nil)
	defer term.Close()
	feed(t, term, "one\r\ntwo\r\nthree\r\nfour")
	if got := term.ScrollbackLen(); got != 2 {
		t.Fatalf("scrollback = %d lines, want 2", got)
	}
	// Scroll back one line: the last scrollback line ("two") appears above the
	// screen, and the bottom screen row ("four") scrolls out of view.
	term.SetScrollOffset(1)
	if got := lineText(term.ViewRow(0)); got != "two" {
		t.Fatalf("ViewRow(0) = %q, want %q", got, "two")
	}
	if got := lineText(term.ViewRow(1)); got != "three" {
		t.Fatalf("ViewRow(1) = %q, want %q", got, "three")
	}
	// Clamps at the scrollback depth.
	term.SetScrollOffset(99)
	if term.ScrollOffset() != term.ScrollbackLen() {
		t.Fatalf("ScrollOffset = %d, want %d", term.ScrollOffset(), term.ScrollbackLen())
	}
	// Negative clamps to 0 (back to the live screen).
	term.SetScrollOffset(-5)
	if term.ScrollOffset() != 0 {
		t.Fatalf("ScrollOffset = %d, want 0", term.ScrollOffset())
	}
	if got := lineText(term.ViewRow(0)); got != "three" {
		t.Fatalf("ViewRow(0) at offset 0 = %q, want %q (live grid)", got, "three")
	}
}
