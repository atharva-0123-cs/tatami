package pane

import (
	"bytes"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestKeyToBytes verifies that the package-level KeyToBytes maps tcell key
// events to the byte sequences a terminal expects.
//
// The table covers: printable runes (with and without Alt), the control keys
// Enter/Backspace/Tab/Esc, the full Ctrl letter range (1-26 via spot checks:
// Ctrl-A/C/Z) plus the special C0 codes (Ctrl-Space, Ctrl-\, ], ^, _), arrows
// in both normal (CSI) and DECCKM (SS3) forms, Home/End/PgUp/PgDn/Delete/F
// keys, and error cases that must return nil (KeyNUL and a nil event).
func TestKeyToBytes(t *testing.T) {
	cases := []struct {
		name      string
		ev        *tcell.EventKey
		appCursor bool
		want      []byte
	}{
		{"letter", tcell.NewEventKey(tcell.KeyRune, 'a', 0), false, []byte("a")},
		{"space", tcell.NewEventKey(tcell.KeyRune, ' ', 0), false, []byte(" ")},
		{"enter", tcell.NewEventKey(tcell.KeyEnter, 0, 0), false, []byte("\r")},
		{"backspace", tcell.NewEventKey(tcell.KeyBackspace, 0, 0), false, []byte{0x7f}},
		{"tab", tcell.NewEventKey(tcell.KeyTab, 0, 0), false, []byte("\t")},
		{"esc", tcell.NewEventKey(tcell.KeyEsc, 0, 0), false, []byte{0x1b}},
		{"ctrl-a", tcell.NewEventKey(tcell.KeyCtrlA, 0, 0), false, []byte{1}},
		{"ctrl-c", tcell.NewEventKey(tcell.KeyCtrlC, 0, 0), false, []byte{3}},
		{"ctrl-z", tcell.NewEventKey(tcell.KeyCtrlZ, 0, 0), false, []byte{0x1a}},
		{"ctrl-space", tcell.NewEventKey(tcell.KeyCtrlSpace, 0, 0), false, []byte{0}},
		{"ctrl-backslash", tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, 0), false, []byte{0x1c}},
		{"ctrl-rightsq", tcell.NewEventKey(tcell.KeyCtrlRightSq, 0, 0), false, []byte{0x1d}},
		{"ctrl-carat", tcell.NewEventKey(tcell.KeyCtrlCarat, 0, 0), false, []byte{0x1e}},
		{"ctrl-underscore", tcell.NewEventKey(tcell.KeyCtrlUnderscore, 0, 0), false, []byte{0x1f}},
		{"arrow-up-normal", tcell.NewEventKey(tcell.KeyUp, 0, 0), false, []byte("\x1b[A")},
		{"arrow-up-app", tcell.NewEventKey(tcell.KeyUp, 0, 0), true, []byte("\x1bOA")},
		{"arrow-left-normal", tcell.NewEventKey(tcell.KeyLeft, 0, 0), false, []byte("\x1b[D")},
		{"arrow-left-app", tcell.NewEventKey(tcell.KeyLeft, 0, 0), true, []byte("\x1bOD")},
		{"home-normal", tcell.NewEventKey(tcell.KeyHome, 0, 0), false, []byte("\x1b[H")},
		{"home-app", tcell.NewEventKey(tcell.KeyHome, 0, 0), true, []byte("\x1bOH")},
		{"pgup", tcell.NewEventKey(tcell.KeyPgUp, 0, 0), false, []byte("\x1b[5~")},
		{"pgdn", tcell.NewEventKey(tcell.KeyPgDn, 0, 0), false, []byte("\x1b[6~")},
		{"delete", tcell.NewEventKey(tcell.KeyDelete, 0, 0), false, []byte("\x1b[3~")},
		{"f2", tcell.NewEventKey(tcell.KeyF2, 0, 0), false, []byte("\x1bOQ")},
		{"f11", tcell.NewEventKey(tcell.KeyF11, 0, 0), false, []byte("\x1b[23~")},
		{"alt-a", tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModAlt), false, []byte{0x1b, 'a'}},
		{"unknown-key", tcell.NewEventKey(tcell.KeyNUL, 0, 0), false, nil},
		{"non-key", nil, false, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := KeyToBytes(c.ev, c.appCursor)
			if c.want == nil {
				if got != nil {
					t.Fatalf("KeyToBytes = %v, want nil", got)
				}
				return
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("KeyToBytes = %v, want %v", got, c.want)
			}
		})
	}
}
