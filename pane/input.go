// Input handling for a pane: converting tcell key events into the byte
// sequences a terminal expects, clipboard copy, bracketed-paste state, and
// raw writes to the PTY master.
package pane

import (
	"fmt"
	"log"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// HandleEvent forwards a tcell key event to the PTY. Returns true if key data was sent.
//
// It converts the event to bytes via KeyToBytes (taking the pane's current
// DECCKM application-cursor mode into account) and writes them to the shell. A
// false return means the event mapped to nothing (e.g. an unknown key) and no
// bytes were written.
func (p *Pane) HandleEvent(ev *tcell.EventKey) bool {
	data := p.KeyToBytes(ev)
	if len(data) > 0 {
		if _, err := p.WriteInput(data); err != nil {
			log.Printf("Error writing input to PTY for pane %s: %v", p.label(), err)
		}
		return true
	}
	return false
}

// AppCursorKeys reports whether application cursor-key mode (DECCKM) is active.
//
// KeyToBytes needs this so arrow/home/end keys are sent in the SS3 form
// (ESC O A) that fullscreen apps expect. The flag is altered by the emulator's
// mode processing, so it is read under the pane mutex.
func (p *Pane) AppCursorKeys() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.term != nil && p.term.decckm
}

// CopyText copies the pane's scrollback + screen to the system clipboard.
//
// It extracts the pane's renderable text (scrollback lines plus the visible
// grid), hands it to the clipboard helper, and returns a status string
// suitable for the status bar ("copied N lines", "copied N lines via <tool>",
// or an error message). Run on the main goroutine.
func (p *Pane) CopyText() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return "", fmt.Errorf("pane not ready")
	}
	text := p.term.allText()
	if text == "" {
		return "nothing to copy", nil
	}
	lines := strings.Count(text, "\n") + 1
	tool, err := CopyToClipboard(text)
	if err != nil {
		return "", err
	}
	if tool == "" {
		return fmt.Sprintf("copied %d lines", lines), nil
	}
	return fmt.Sprintf("copied %d lines via %s", lines, tool), nil
}

// Paste reports whether the pane's shell has enabled bracketed paste
// (DECSET 2004), so the UI should wrap pasted input in ESC[200~/ESC[201~.
//
// When the shell turns mode 2004 on it expects the app to delimit every paste;
// these markers let the shell treat the pasted bytes as data instead of
// keystrokes (so newlines in a paste do not execute commands).
func (p *Pane) Paste() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.term != nil && p.term.bracketedPaste
}

// KeyToBytes converts a tcell event into the byte sequence expected by a terminal.
//
// Pane-level wrapper: captures the pane's current DECCKM mode and delegates to
// the package-level function.
func (p *Pane) KeyToBytes(ev *tcell.EventKey) []byte {
	return KeyToBytes(ev, p.AppCursorKeys())
}

// KeyToBytes converts a tcell event into bytes.
//
// The translation rules mirror what a real terminal driver does:
//   - printable runes (KeyRune) are transmitted as-is, with Meta/Alt prefixing
//     ESC (0x1b);
//   - Enter/Backspace/Tab map to CR, DEL (0x7f) and TAB;
//   - arrows/home/end use CSI (ESC [ ...) or, when appCursor (DECCKM) is set,
//     SS3 (ESC O ...);
//   - function keys, page keys, Delete and Insert use their xterm codes;
//   - Ctrl+letter pairs map to bytes 1..26; the special Ctrl codes (space,
//     \, ], ^, _) map to their C0 byte equivalents.
//
// Returns nil for events that have no byte representation (e.g. KeyNUL).
func KeyToBytes(ev *tcell.EventKey, appCursor bool) []byte {
	if ev == nil {
		return nil
	}

	// Plain printable characters come through as KeyRune with the rune set.
	if ev.Key() == tcell.KeyRune {
		r := ev.Rune()
		if r == 0 {
			return nil
		}
		if ev.Modifiers()&tcell.ModAlt != 0 {
			return []byte{0x1b, byte(r)}
		}
		return []byte(string(r))
	}

	arrow := func(csi, ss3 byte) []byte {
		if appCursor {
			return []byte{0x1b, 'O', ss3}
		}
		return []byte{0x1b, '[', csi}
	}

	switch ev.Key() {
	case tcell.KeyEnter:
		return []byte{'\r'}
	case tcell.KeyBackspace:
		return []byte{0x7f}
	case tcell.KeyTab:
		return []byte{'\t'}
	case tcell.KeyEsc:
		return []byte{0x1b}
	case tcell.KeyUp:
		return arrow('A', 'A')
	case tcell.KeyDown:
		return arrow('B', 'B')
	case tcell.KeyRight:
		return arrow('C', 'C')
	case tcell.KeyLeft:
		return arrow('D', 'D')
	case tcell.KeyHome:
		if appCursor {
			return []byte{0x1b, 'O', 'H'}
		}
		return []byte("\x1b[H")
	case tcell.KeyEnd:
		if appCursor {
			return []byte{0x1b, 'O', 'F'}
		}
		return []byte("\x1b[F")
	case tcell.KeyPgUp:
		return []byte("\x1b[5~")
	case tcell.KeyPgDn:
		return []byte("\x1b[6~")
	case tcell.KeyDelete:
		return []byte("\x1b[3~")
	case tcell.KeyInsert:
		return []byte("\x1b[2~")
	case tcell.KeyF1:
		return []byte("\x1bOP")
	case tcell.KeyF2:
		return []byte("\x1bOQ")
	case tcell.KeyF3:
		return []byte("\x1bOR")
	case tcell.KeyF4:
		return []byte("\x1bOS")
	case tcell.KeyF5:
		return []byte("\x1b[15~")
	case tcell.KeyF6:
		return []byte("\x1b[17~")
	case tcell.KeyF7:
		return []byte("\x1b[18~")
	case tcell.KeyF8:
		return []byte("\x1b[19~")
	case tcell.KeyF9:
		return []byte("\x1b[20~")
	case tcell.KeyF10:
		return []byte("\x1b[21~")
	case tcell.KeyF11:
		return []byte("\x1b[23~")
	case tcell.KeyF12:
		return []byte("\x1b[24~")
	case tcell.KeyCtrlSpace:
		return []byte{0}
	case tcell.KeyCtrlBackslash: // Ctrl-\
		return []byte{0x1c}
	case tcell.KeyCtrlRightSq: // Ctrl-]
		return []byte{0x1d}
	case tcell.KeyCtrlCarat: // Ctrl-^
		return []byte{0x1e}
	case tcell.KeyCtrlUnderscore: // Ctrl-_
		return []byte{0x1f}
	}

	// Ctrl+A..Ctrl+Z map to bytes 1..26 (KeyCtrlA == 65).
	if ev.Key() >= tcell.KeyCtrlA && ev.Key() <= tcell.KeyCtrlZ {
		return []byte{byte(ev.Key() - tcell.KeyCtrlA + 1)}
	}

	return nil
}

// WriteInput writes raw bytes to the PTY master.
//
// Serialized with ptyMtx so a key sequence and a bracketed-paste delimiter
// never interleave. Returns an error only if the PTY is closed or the write
// itself fails.
func (p *Pane) WriteInput(data []byte) (int, error) {
	p.ptyMtx.Lock()
	defer p.ptyMtx.Unlock()
	if p.pty == nil {
		return 0, fmt.Errorf("pty closed")
	}
	return p.pty.Write(data)
}
