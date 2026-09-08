package pane

import (
	"bytes"
	"testing"

	"github.com/nsf/termbox-go"
)

func TestKeyToBytes(t *testing.T) {
	cases := []struct {
		name      string
		ev        termbox.Event
		appCursor bool
		want      []byte
	}{
		{"letter", termbox.Event{Type: termbox.EventKey, Ch: 'a', Key: 0}, false, []byte("a")},
		{"space", termbox.Event{Type: termbox.EventKey, Ch: ' '}, false, []byte(" ")},
		{"enter", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyEnter}, false, []byte("\r")},
		{"backspace", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyBackspace}, false, []byte{0x7f}},
		{"backspace2", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyBackspace2}, false, []byte{0x08}},
		{"tab", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyTab}, false, []byte("\t")},
		{"esc", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyEsc}, false, []byte{0x1b}},
		{"ctrl-c", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyCtrlC}, false, []byte{3}},
		{"ctrl-z", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyCtrlZ}, false, []byte{0x1a}},
		{"ctrl-space", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyCtrlSpace}, false, []byte{0}},
		{"arrow-up-normal", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyArrowUp}, false, []byte("\x1b[A")},
		{"arrow-up-app", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyArrowUp}, true, []byte("\x1bOA")},
		{"arrow-left-normal", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyArrowLeft}, false, []byte("\x1b[D")},
		{"arrow-left-app", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyArrowLeft}, true, []byte("\x1bOD")},
		{"home-normal", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyHome}, false, []byte("\x1b[H")},
		{"home-app", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyHome}, true, []byte("\x1bOH")},
		{"pgup", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyPgup}, false, []byte("\x1b[5~")},
		{"pgdn", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyPgdn}, false, []byte("\x1b[6~")},
		{"delete", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyDelete}, false, []byte("\x1b[3~")},
		{"f2", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyF2}, false, []byte("\x1bOQ")},
		{"f11", termbox.Event{Type: termbox.EventKey, Key: termbox.KeyF11}, false, []byte("\x1b[23~")},
		{"alt-a", termbox.Event{Type: termbox.EventKey, Ch: 'a', Mod: termbox.ModAlt}, false, []byte{0x1b, 'a'}},
		{"unknown-key", termbox.Event{Type: termbox.EventKey, Key: termbox.MouseLeft}, false, nil},
		{"non-key", termbox.Event{Type: termbox.EventResize}, false, nil},
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
