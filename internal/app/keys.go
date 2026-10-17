// Multiplexer key handling: the prefix/resize/copy command parser (handleKey)
// and the passthrough used for unhandled events and bracketed paste
// (forwardKey). The mode state these methods read and write lives on the App.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gdamore/tcell/v2"

	"github.com/atharva-0123-cs/tatami/falkor"
	"github.com/atharva-0123-cs/tatami/window"
)

// handleKey processes the multiplexer's prefix and resize commands.
//
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
//     simply ends prefix mode without an action.
//  5. Otherwise the event is forwarded to the shell.
func (a *App) handleKey(ev *tcell.EventKey) bool {
	// Escape cancels prefix and resize mode.
	if ev.Key() == tcell.KeyEsc {
		if a.copyMode {
			a.copyMode = false
			a.copyOffset = 0
			if focused := a.wm.FocusedPane(); focused != nil {
				focused.Scroll(0)
			}
			return true
		}
		if a.resizeMode || a.prefixActive {
			a.resizeMode = false
			a.prefixActive = false
			return true
		}
		return false // forward Esc to the shell
	}

	// The configured prefix toggles prefix mode.
	if ev.Key() == a.prefixKey {
		if a.resizeMode {
			a.resizeMode = false
		}
		a.prefixActive = !a.prefixActive
		return true
	}

	// Copy mode: j/k scroll, y copies visible text, q exits.
	if a.copyMode {
		if ev.Key() == tcell.KeyRune {
			switch ev.Rune() {
			case 'j':
				if a.copyOffset > 0 {
					a.copyOffset--
					if focused := a.wm.FocusedPane(); focused != nil {
						focused.Scroll(a.copyOffset)
					}
				}
				return true
			case 'k':
				a.copyOffset++
				if focused := a.wm.FocusedPane(); focused != nil {
					focused.Scroll(a.copyOffset)
				}
				return true
			case 'y':
				a.copyMode = false
				a.copyOffset = 0
				if focused := a.wm.FocusedPane(); focused != nil {
					focused.Scroll(0)
					msg, err := focused.CopyText()
					if err != nil {
						a.SetErrorMsg(err.Error())
					} else {
						a.SetErrorMsg(msg)
					}
				}
				return true
			case 'q':
				a.copyMode = false
				a.copyOffset = 0
				if focused := a.wm.FocusedPane(); focused != nil {
					focused.Scroll(0)
				}
				return true
			}
		}
		// Any other key exits copy mode.
		a.copyMode = false
		a.copyOffset = 0
		if focused := a.wm.FocusedPane(); focused != nil {
			focused.Scroll(0)
		}
		return true
	}

	// Resize mode: h/j/k/l adjust the focused pane's proportions.
	if a.resizeMode {
		a.resizeMode = false
		if ev.Key() == tcell.KeyRune {
			switch ev.Rune() {
			case 'h':
				a.wm.DecreaseSize(window.SplitVertical)
			case 'l':
				a.wm.IncreaseSize(window.SplitVertical)
			case 'k':
				a.wm.DecreaseSize(window.SplitHorizontal)
			case 'j':
				a.wm.IncreaseSize(window.SplitHorizontal)
			}
		}
		return true
	}

	// Prefix mode commands. Only unmodified characters (KeyRune) act as
	// letter commands; any other key simply ends prefix mode.
	if a.prefixActive {
		a.prefixActive = false
		if ev.Key() == tcell.KeyRune {
			ch := ev.Rune()

			// Numbered pane selection: 1-9 jumps to pane N.
			if ch >= '1' && ch <= '9' {
				idx := int(ch - '0')
				panes := a.wm.CollectPanes()
				if idx >= 1 && idx <= len(panes) {
					a.wm.FocusPane(panes[idx-1])
				} else {
					a.SetErrorMsg(fmt.Sprintf("Invalid pane: %d (total %d)", idx, len(panes)))
				}
				return true
			}

			switch ch {
			case 'c', '-': // Horizontal split (top/bottom)
				if err := a.wm.NewPane(); err != nil {
					slog.Error("split failed", "dir", "horizontal", "err", err)
					a.SetErrorMsg(fmt.Sprintf("Split failed: %v", err))
				}
				return true
			case 'C', '|': // Vertical split (left/right)
				if err := a.wm.NewPaneVertical(); err != nil {
					slog.Error("split failed", "dir", "vertical", "err", err)
					a.SetErrorMsg(fmt.Sprintf("Split failed: %v", err))
				}
				return true
			case 'x': // Close current pane
				if a.wm.CloseCurrentPane() {
					a.shouldQuit = true
				}
				return true
			case 'h':
				a.wm.FocusLeft()
				return true
			case 'j':
				a.wm.FocusDown()
				return true
			case 'k':
				a.wm.FocusUp()
				return true
			case 'l':
				a.wm.FocusRight()
				return true
			case 'z':
				a.wm.Zoom()
				return true
			case 'o':
				a.wm.RotateFocused(1)
				return true
			case '}':
				a.wm.SwapNext()
				return true
			case '{':
				a.wm.SwapPrev()
				return true
			case 'r':
				a.resizeMode = true
				return true
			case 'q':
				a.shouldQuit = true
				return true
			case 't':
				// FalkorDB Connection Test Command
				if falkor.GlobalClient != nil {
					// A simple cypher query to test connectivity:
					// Create a dummy node and return it
					res, err := falkor.GlobalClient.Query(context.Background(), "CREATE (:Test {name: 'tatami'}) RETURN 1")
					if err != nil {
						a.SetErrorMsg(fmt.Sprintf("FalkorDB Test Failed: %v", err))
					} else {
						a.SetErrorMsg(fmt.Sprintf("FalkorDB Test Success: %v", res))
					}
				} else {
					a.SetErrorMsg("FalkorDB Client not initialized")
				}
				return true
			case 'w':
				a.wm.NextWindow()
				return true
			case 'W', 'p':
				a.wm.PrevWindow()
				return true
			case 'n':
				if err := a.wm.NewWindow(); err != nil {
					slog.Error("failed to create window", "err", err)
					a.SetErrorMsg(fmt.Sprintf("New window failed: %v", err))
				}
				return true
			case '[':
				a.copyMode = true
				a.copyOffset = 0
				if focused := a.wm.FocusedPane(); focused != nil {
					focused.Scroll(0)
				}
				return true
			}
		}

		// Arrow keys during prefix mode - move focus.
		switch ev.Key() {
		case tcell.KeyLeft:
			a.wm.FocusLeft()
			return true
		case tcell.KeyRight:
			a.wm.FocusRight()
			return true
		case tcell.KeyUp:
			a.wm.FocusUp()
			return true
		case tcell.KeyDown:
			a.wm.FocusDown()
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
func (a *App) forwardKey(ev *tcell.EventKey) {
	if focused := a.wm.FocusedPane(); focused != nil {
		focused.HandleEvent(ev)
	}
}