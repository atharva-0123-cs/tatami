// CSI/ESC sequence parsing and dispatch for the terminal emulator.
package term

import (
	"strconv"
	"strings"
)

// parseEscape parses one escape sequence at buf[0] == ESC. Returns bytes
// consumed, or -1 when truncated (wait for more input).
//
// The sequence is dispatched on its second byte: CSI (ESC [) goes to
// parseCSI, OSC (ESC ]) and DCS/SOS/PM/APC (ESC P X ^ _) are consumed up to
// their terminator, charset designation (ESC ( / ) ), cursor save/restore
// (ESC 7/8), reverse index (ESC M), linefeed (ESC D), index-to-column-0
// (ESC E), HTS (ESC H), RIS (ESC c), single shifts (ESC N/O) and a catch-all
// for other 2-byte sequences are handled inline.
func (t *Terminal) parseEscape(buf []byte) int {
	if len(buf) < 2 {
		return -1
	}
	switch buf[1] {
	case '[':
		return t.parseCSI(buf)
	case ']':
		return t.consumeVTOSC(buf)
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
func (t *Terminal) setCharset(g1 bool, b byte) {
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
func (t *Terminal) clampX(x int) int {
	if x < 0 {
		return 0
	}
	if x >= t.cols {
		return t.cols - 1
	}
	return x
}

// clampY clamps a row index into the range [0, rows).
func (t *Terminal) clampY(y int) int {
	if y < 0 {
		return 0
	}
	if y >= t.rows {
		return t.rows - 1
	}
	return y
}

// parseCSI parses "ESC [ params intermediates final".
//
// Scans parameter bytes (0x30-0x3F) and intermediate bytes (0x20-0x2F), then
// dispatches on the final character: cursor movement (H f A B C D E F G d),
// erase (J K), insert/delete (L M P X @), SGR (m), tab stops (g), scroll (S T),
// save/restore cursor (s u), scroll region (r), attribute/DA/CPR replies
// (c n for the emulator side) and mode setting (h l). Private-mode sequences
// are routed through setMode. Unknown finals are ignored.
func (t *Terminal) parseCSI(buf []byte) int {
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
	if len(params) > 0 && (params[0] == '?' || params[0] == '>' ||
		params[0] == '=' || params[0] == '!') {
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
//
// Empty fields appear in sequences like "ESC [ H" (params "") and
// "ESC [ ; m"; the -1 sentinel lets pn distinguish "absent" from "0".
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

// setMode handles SM/RM (CSI h/l), including private modes.
//
// Tracks the modes the emulator actually represents: 25 (DECTCEM cursor
// visibility), 1 (DECCKM app cursor keys), 7 (autowrap) and the alternate
// screen (47/1047/1049) and bracketed paste (2004). Non-private sets and
// mouse/focus-reporting modes are ignored.
func (t *Terminal) setMode(priv string, nums []int, on bool) {
	if priv == "" || priv != "?" || len(nums) == 0 {
		return
	}
	for _, m := range nums {
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
//
// Fullscreen TUI apps (vim, less, htop) enter the alt screen on startup and
// leave on exit; the main buffer is preserved so the pre-app screen returns
// intact when the app quits. The alt grid starts blank and its cursor is reset
// to (0,0).
func (t *Terminal) enterAlt() {
	if t.altOn {
		return
	}
	t.msx, t.msy = t.x, t.y
	t.alt = make([]Cell, t.cols*t.rows)
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
//
// The main-buffer cursor is restored from msx/msy and the scroll region is
// reset for the whole screen.
func (t *Terminal) exitAlt() {
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
//
// Some shells probe the terminal with CSI c at startup and fall back to a
// reduced feature set if the answer is unrecognized; answering VT100-with-AVP
// keeps them in full ANSI mode.
func (t *Terminal) deviceAttrs() {
	t.sendResponse([]byte("\x1b[?6c"))
}

// sendResponse writes a reply (DA/CPR) back to the shell via the respond callback.
func (t *Terminal) sendResponse(b []byte) {
	if t.respond != nil {
		t.respond(b)
	}
}

// consumeVTOSC consumes "ESC ] ... (BEL | ESC \)".
//
// OSC payloads (title, clipboard, hyperlinks, OSC133 shell integration) are
// parsed when they carry a title (OSC 0/2); other payloads are consumed and
// discarded. Returns the number of bytes consumed, or -1 when the terminating
// byte has not arrived yet.
func (t *Terminal) consumeVTOSC(buf []byte) int {
	// Find the payload between ']' and the terminator (BEL or ST).
	payloadStart := -1
	for i := 2; i < len(buf); i++ {
		if buf[i] == 0x07 {
			// BEL terminates the OSC.
			if payloadStart >= 0 {
				t.handleOSC(buf[payloadStart:i])
			}
			return i + 1
		}
		if buf[i] == 0x1b {
			if i+1 >= len(buf) {
				return -1
			}
			if buf[i+1] == '\\' {
				// ST terminates the OSC.
				if payloadStart >= 0 {
					t.handleOSC(buf[payloadStart:i])
				}
				return i + 2
			}
		}
		if payloadStart < 0 {
			payloadStart = i
		}
	}
	return -1
}

// handleOSC processes the payload of an OSC sequence. OSC 0 and OSC 2 set
// the window title; all other OSC codes are ignored.
func (t *Terminal) handleOSC(payload []byte) {
	s := string(payload)
	// OSC codes are "N;text" where N is the code number.
	semi := -1
	for i, c := range s {
		if c == ';' {
			semi = i
			break
		}
	}
	if semi < 0 {
		return
	}
	code := s[:semi]
	title := s[semi+1:]
	switch code {
	case "0", "2":
		t.title = title
	}
}

// consumeVTST consumes DCS/SOS/PM/APC up to ST or BEL.
//
// Same shape as consumeVTOSC but for the "string"-type sequences (DCS uses
// these for sixel and status reports; those are ignored here). Returns bytes
// consumed or -1 when truncated.
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
//
// When G1 (or a shifted G0) is programmed with charset "0", the printable
// bytes in this range render as box-drawing/special glyphs. vim and other
// fullscreen apps rely on this for their text drawing.
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
