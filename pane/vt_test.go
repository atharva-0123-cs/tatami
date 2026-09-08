package pane

import (
	"strings"
	"testing"
)

func newTestTerm() *terminal {
	return newTerminal(20, 5, nil)
}

func feed(t *testing.T, term *terminal, in string) {
	t.Helper()
	term.process([]byte(in))
}

func TestPutBasic(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "hello")
	if got := term.dump(); got != "hello" {
		t.Fatalf("dump = %q, want %q", got, "hello")
	}
}

func TestCursorMovement(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "\x1b[2;3H")
	feed(t, term, "XY")
	lines := strings.Split(term.dump(), "\n")
	if len(lines) < 2 || lines[1] != "  XY" {
		t.Fatalf("row 2 = %q, want %q", lines[1], "  XY")
	}
	feed(t, term, "\x1b[1A\x1b[1D")
	if term.x != 3 || term.y != 0 {
		t.Fatalf("cursor = (%d,%d), want (3,0)", term.x, term.y)
	}
	feed(t, term, "\x1b[3D")
	if term.x != 0 {
		t.Fatalf("cursor x = %d, want 0", term.x)
	}
}

func TestEraseLine(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "abcdefgh")
	feed(t, term, "\x1b[1D\x1b[1D\x1b[K")
	if got := term.dump(); got != "abcdef" {
		t.Fatalf("dump = %q, want %q", got, "abcdef")
	}
}

func TestEraseDisplay(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "abc\r\ndef")
	feed(t, term, "\x1b[2J")
	if got := term.dump(); got != "" {
		t.Fatalf("dump = %q, want empty", got)
	}
}

func TestAltScreen(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "main")
	feed(t, term, "\x1b[?1049h")
	if !term.altOn {
		t.Fatal("altOn not set")
	}
	feed(t, term, "ALT")
	if got := term.dump(); got != "ALT" {
		t.Fatalf("alt dump = %q, want %q", got, "ALT")
	}
	feed(t, term, "\x1b[?1049l")
	if term.altOn {
		t.Fatal("altOn not cleared")
	}
	if got := term.dump(); got != "main" {
		t.Fatalf("main dump = %q, want %q", got, "main")
	}
}

func TestACSGraphics(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "\x1b)0") // program G1 with DEC special graphics
	feed(t, term, "\x0e")   // SO: shift G1 in
	feed(t, term, "q")      // -> box drawing horizontal
	feed(t, term, "\x0f")   // SI: shift G0 in
	feed(t, term, "a")
	if got := term.dump(); got != "\u2500a" {
		t.Fatalf("dump = %q, want %q", got, "\u2500a")
	}
}

func TestWideChars(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "\u4f60a") // CJK wide, then narrow
	c0 := *term.at(0, 0)
	if c0.ch != '\u4f60' {
		t.Fatalf("cell0 = %q, want wide rune", c0.ch)
	}
	if !term.at(1, 0).wide {
		t.Fatal("continuation cell not marked wide")
	}
	if got := term.at(2, 0).ch; got != 'a' {
		t.Fatalf("cell2 = %q, want %q", got, 'a')
	}
	if term.x != 3 {
		t.Fatalf("cursor x = %d, want 3", term.x)
	}
	if got := term.dump(); got != "\u4f60 a" {
		t.Fatalf("dump = %q, want %q", got, "\u4f60 a")
	}
}

func TestWideCharsWrapAtLastColumn(t *testing.T) {
	term := newTerminal(5, 3, nil)
	feed(t, term, "\x1b[5G") // cursor to last column (x=4)
	feed(t, term, "\u4f60")
	if term.y != 1 || term.x != 2 {
		t.Fatalf("cursor = (%d,%d), want (2,1)", term.x, term.y)
	}
	if got := term.at(0, 1).ch; got != '\u4f60' {
		t.Fatalf("cell = %q, want wide rune", got)
	}
}

func TestCombiningMarksDropped(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "e\u0301")
	if got := term.dump(); got != "e" {
		t.Fatalf("dump = %q, want %q", got, "e")
	}
	if term.x != 1 {
		t.Fatalf("cursor x = %d, want 1", term.x)
	}
}

func TestScrollbackAndAllText(t *testing.T) {
	term := newTerminal(5, 2, nil)
	feed(t, term, "123\r\n456\r\n7")
	if got := term.allText(); got != "123\n456\n7" {
		t.Fatalf("allText = %q, want %q", got, "123\n456\n7")
	}
}

func TestSGRColors(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "\x1b[38;5;196mX")
	if c := *term.at(0, 0); c.fg.idx != 196 || c.ch != 'X' {
		t.Fatalf("cell = %+v, want fg idx 196 ch X", c)
	}
	feed(t, term, "\x1b[0mY")
	if c := *term.at(1, 0); !c.fg.def || c.ch != 'Y' {
		t.Fatalf("cell = %+v, want default fg reset", c)
	}
}

func TestSGRTruecolor(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "\x1b[38;2;255;0;128mX")
	c := *term.at(0, 0)
	if c.fg.rgb != true || c.fg.r != 255 || c.fg.g != 0 || c.fg.b != 128 {
		t.Fatalf("fg = %+v, want truecolor 255;0;128", c.fg)
	}
	feed(t, term, "\x1b[48;2;1;2;3mY")
	c = *term.at(1, 0)
	if c.bg.rgb != true || c.bg.r != 1 || c.bg.g != 2 || c.bg.b != 3 {
		t.Fatalf("bg = %+v, want truecolor 1;2;3", c.bg)
	}
}

func TestBracketedPasteMode(t *testing.T) {
	term := newTestTerm()
	if term.bracketedPaste {
		t.Fatal("bracketed paste should default to off")
	}
	feed(t, term, "\x1b[?2004h")
	if !term.bracketedPaste {
		t.Fatal("bracketed paste should be on after CSI ?2004 h")
	}
	feed(t, term, "\x1b[?2004l")
	if term.bracketedPaste {
		t.Fatal("bracketed paste should be off after CSI ?2004 l")
	}
}

func TestScrollRegion(t *testing.T) {
	term := newTestTerm()
	feed(t, term, "\x1b[2;4r")
	feed(t, term, "\x1b[2;1H")
	feed(t, term, "abc\r\ndef\r\nghi")
	feed(t, term, "\n") // scroll once inside region rows 2-4
	rows := strings.Split(term.dump(), "\n")
	if len(rows) != 3 || rows[0] != "" || rows[1] != "def" || rows[2] != "ghi" {
		t.Fatalf("dump = %q, want rows (empty/def/ghi)", term.dump())
	}
	// Row above the region must not accept the scrolled line.
	if got := term.at(0, 0).ch; got != ' ' {
		t.Fatalf("row 1 cell = %q, want space (outside region untouched)", got)
	}
	if got := term.at(0, 3).ch; got != ' ' {
		t.Fatalf("bottom of region not cleared, got %q", got)
	}
}

func TestCursorVisibleMode(t *testing.T) {
	term := newTestTerm()
	if !term.cursorVisible {
		t.Fatal("cursor should default to visible")
	}
	feed(t, term, "\x1b[?25l")
	if term.cursorVisible {
		t.Fatal("cursor should be hidden after CSI ?25 l")
	}
	feed(t, term, "\x1b[?25h")
	if !term.cursorVisible {
		t.Fatal("cursor should be visible after CSI ?25 h")
	}
}
