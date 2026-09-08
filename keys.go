// Multiplexer key handling: the prefix/resize command parser (handleKey) and
// the passthrough used for unhandled events and bracketed paste (forwardKey).
// The mode state these functions read and write lives in state.go.
package main

import (
	"fmt"
	"log"

	"github.com/gdamore/tcell/v2"

	"github.com/atharva-0123-cs/tview-demo/layout"
) // handleKey processes the multiplexer's prefix and resize commands.
// It returns true when the multiplexer consumed the event (a command ran or a
// mode was toggled) and false when the caller should forward the event to the
// focused pane's shell. The precedence is:
//
//  1. Escape cancels prefix/resize mode (and is passed to the shell if no
//     mode was active).
//  2. Ctrl+A stops resize mode and toggles prefix mode.
//  3. While resize mode is active, a single h/j/k/l adjusts the focused
//     pane's size (h/l change the vertical split, k/j the horizontal split)
//     and ends the mode; any other key just ends it.
//  4. While prefix mode is active, run the bound command (splits, close,
//     focus, zoom, rotate, swap, resize, jump, quit). Only plain characters
//     (KeyRune) trigger letter commands; arrow keys move focus; any other key
//     simply ends prefix mode without an action, mirroring the historical
//     termbox behavior where modified keys had Ch == 0.
//  5. Otherwise the event is forwarded to the shell.
func handleKey(ev *tcell.EventKey, prefix tcell.Key) bool {
	// Escape cancels prefix and resize mode.
	if ev.Key() == tcell.KeyEsc {
		if resizeMode || prefixActive {
			resizeMode = false
			prefixActive = false
			return true
		}
		return false // forward Esc to the shell
	}

	// The configured prefix toggles prefix mode.
	if ev.Key() == prefix {
		if resizeMode {
			resizeMode = false
		}
		prefixActive = !prefixActive
		return true
	}

	// Resize mode: h/j/k/l adjust the focused pane's proportions.
	if resizeMode {
		resizeMode = false
		if ev.Key() == tcell.KeyRune {
			switch ev.Rune() {
			case 'h':
				wm.DecreaseSize(layout.SplitVertical)
			case 'l':
				wm.IncreaseSize(layout.SplitVertical)
			case 'k':
				wm.DecreaseSize(layout.SplitHorizontal)
			case 'j':
				wm.IncreaseSize(layout.SplitHorizontal)
			}
		}
		return true
	}

	// Prefix mode commands. Only unmodified characters (KeyRune) act as
	// letter commands; any other key simply ends prefix mode.
	if prefixActive {
		prefixActive = false
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()

			// Numbered pane selection: 1-9 jumps to pane N.
			if ch >= '1' && ch <= '9' {
				idx := int(ch - '0')
				panes := wm.CollectPanes()
				if idx >= 1 && idx <= len(panes) {
					wm.FocusPane(panes[idx-1])
				} else {
					setErrorMessage(fmt.Sprintf("Invalid pane: %d (total %d)", idx, len(panes)))
				}
				return true
			}

			switch ch {
			case 'c', '-': // Horizontal split (top/bottom)
				if err := wm.NewPane(); err != nil {
					log.Printf("Error creating pane: %v", err)
					setErrorMessage(fmt.Sprintf("Split failed: %v", err))
				}
				return true
			case 'C', '|': // Vertical split (left/right)
				if err := wm.NewPaneVertical(); err != nil {
					log.Printf("Error creating vertical pane: %v", err)
					setErrorMessage(fmt.Sprintf("Split failed: %v", err))
				}
				return true
			case 'x': // Close current pane
				if wm.CloseCurrentPane() {
					shouldQuit = true
				}
				return true
			case 'h':
				wm.FocusLeft()
				return true
			case 'j':
				wm.FocusDown()
				return true
			case 'k':
				wm.FocusUp()
				return true
			case 'l':
				wm.FocusRight()
				return true
			case 'z':
				wm.Zoom()
				return true
			case 'o':
				wm.RotateFocused(1)
				return true
			case '}':
				wm.SwapNext()
				return true
			case '{':
				wm.SwapPrev()
				return true
			case 'r':
				resizeMode = true
				return true
			case 'q':
				shouldQuit = true
				return true
			}
		}

		// Arrow keys during prefix mode - move focus.
		switch ev.Key() {
		case tcell.KeyLeft:
			wm.FocusLeft()
			return true
		case tcell.KeyRight:
			wm.FocusRight()
			return true
		case tcell.KeyUp:
			wm.FocusUp()
			return true
		case tcell.KeyDown:
			wm.FocusDown()
			return true
		}

		// Any other key ends prefix mode without action.
		return true
	}

	// Default: pass the key through to the running shell.
	return false
}

// forwardKey hands an uninterpreted key event to the focused pane's shell.
//
// It is used both for keys handleKey did not consume and for every key while
// a bracketed paste is active. The focused pane translates the tcell event
// into the byte sequence a terminal expects and writes it to the PTY; if no
// pane is focused the event is dropped.
func forwardKey(ev *tcell.EventKey) {
	if focused := wm.FocusedPane(); focused != nil {
		focused.HandleEvent(ev)
	}
}
