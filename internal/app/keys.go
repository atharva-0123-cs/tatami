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
	// The command prompt is modal: it consumes every key while open.
	if a.cmdMode {
		return a.handleCommandInput(ev)
	}

	// The pane picker/search overlay is modal.
	if a.pickKind != pickNone {
		return a.handlePickerInput(ev)
	}

	// Graph overlays are modal: they consume every key while open.
	if a.graphMode == graphInput {
		return a.handleGraphInput(ev)
	}
	if a.graphMode == graphResultMode {
		return a.handleGraphResult(ev)
	}

	// Escape cancels prefix and resize mode.
	if ev.Key() == tcell.KeyEsc {
		if a.copyMode {
			a.exitCopyMode()
			return true
		}
		if a.resizeMode || a.prefixActive {
			a.resizeMode = false
			a.prefixActive = false
			return true
		}
		return false // forward Esc to the shell
	}

	// The configured prefix toggles prefix mode (and leaves copy mode).
	if ev.Key() == a.prefixKey {
		if a.resizeMode {
			a.resizeMode = false
		}
		if a.copyMode {
			a.exitCopyMode()
		}
		a.prefixActive = !a.prefixActive
		return true
	}

	// Copy mode: scroll the focused pane's scrollback (j/k, arrows, PgUp/PgDn,
	// Home/End, g/G), copy with y, and exit with q/Esc or any unhandled key.
	if a.copyMode {
		focused := a.wm.FocusedPane()
		switch {
		case ev.Key() == tcell.KeyUp:
			a.scrollCopyBy(-1)
			return true
		case ev.Key() == tcell.KeyDown:
			a.scrollCopyBy(1)
			return true
		case ev.Key() == tcell.KeyPgUp:
			a.scrollCopyBy(-a.copyPage())
			return true
		case ev.Key() == tcell.KeyPgDn:
			a.scrollCopyBy(a.copyPage())
			return true
		case ev.Key() == tcell.KeyHome:
			a.scrollCopyTo(a.maxCopyOffset())
			return true
		case ev.Key() == tcell.KeyEnd:
			a.scrollCopyTo(0)
			return true
		case ev.Key() == tcell.KeyRune:
			switch ev.Rune() {
			case 'j':
				a.scrollCopyBy(1)
				return true
			case 'k':
				a.scrollCopyBy(-1)
				return true
			case 'g':
				a.scrollCopyTo(a.maxCopyOffset())
				return true
			case 'G':
				a.scrollCopyTo(0)
				return true
			case 'y':
				a.copyMode = false
				a.copyOffset = 0
				if focused != nil {
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
				a.exitCopyMode()
				return true
			}
		}
		// Any other key exits copy mode.
		a.exitCopyMode()
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

			// Numbered window selection: 1-9 switches to window N.
			if ch >= '1' && ch <= '9' {
				idx := int(ch - '0')
				if a.wm.HasWindow(idx) {
					a.wm.SwitchWindow(idx)
				} else {
					a.SetErrorMsg(fmt.Sprintf("Invalid window: %d (total %d)", idx, a.wm.WindowCount()))
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
			case ':':
				// Open the command prompt (command.go). The Cypher query
				// prompt (graph.go) is reached with the `cypher` command.
				a.openCommandPrompt()
				return true
			case 'g':
				// Index the working directory into FalkorDB.
				a.indexProject()
				return true
			case 'm':
				// Open the Cypher prompt directly (graph memory), without
				// going through command mode's `cypher` command.
				a.openGraphPrompt()
				return true
			case 's':
				// Save the current workspace as a FalkorDB session.
				a.openSessionPrompt(graphActSave)
				return true
			case 'R':
				// Restore a workspace from a saved FalkorDB session.
				a.openSessionPrompt(graphActRestore)
				return true
			case 'G':
				// Show the focused pane's Git status.
				a.showGitStatus()
				return true
			case 'd':
				// Show the focused pane's uncommitted Git diff.
				a.showGitDiff()
				return true
			case ';':
				// Open the command prompt (command mode).
				a.openCommandPrompt()
				return true
			case 'P':
				// Open the pane selection UI.
				a.openPanePicker()
				return true
			case 'f':
				// Open pane content search.
				a.openPaneSearch()
				return true
			case 'S':
				// Toggle synchronized input across the window's panes.
				a.toggleSync()
				return true
			case 'w':
				if err := a.wm.NewWindow(); err != nil {
					slog.Error("failed to create window", "err", err)
					a.SetErrorMsg(fmt.Sprintf("New window failed: %v", err))
				}
				return true
			case 'n':
				a.wm.NextWindow()
				return true
			case 'p', 'W':
				a.wm.PrevWindow()
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
	if a.syncOn() {
		for _, p := range a.wm.CollectPanes() {
			p.HandleEvent(ev)
		}
		return
	}
	if focused := a.wm.FocusedPane(); focused != nil {
		focused.HandleEvent(ev)
	}
}

// ------------------------------------------------------- copy/scroll mode ---

// copyPage is the number of lines a PageUp/PageDown moves in copy mode: one
// screenful minus an overlap row, so context is preserved across pages.
func (a *App) copyPage() int {
	n := 1
	if focused := a.wm.FocusedPane(); focused != nil {
		if h := focused.Height(); h > 1 {
			n = h - 1
		}
	}
	return n
}

// maxCopyOffset is the largest useful scroll offset (the full scrollback).
func (a *App) maxCopyOffset() int {
	if focused := a.wm.FocusedPane(); focused != nil {
		return focused.ScrollbackLen()
	}
	return 0
}

// scrollCopyBy moves the copy-mode view by delta lines: positive scrolls toward
// the live screen (down/newer), negative toward older scrollback (up). The
// pane clamps the result, so the cached offset never runs past the scrollback.
func (a *App) scrollCopyBy(delta int) {
	focused := a.wm.FocusedPane()
	if focused == nil {
		return
	}
	focused.Scroll(focused.ScrollOffset() - delta)
	a.copyOffset = focused.ScrollOffset()
}

// scrollCopyTo jumps the copy-mode view to an absolute offset (older = larger),
// clamped by the pane.
func (a *App) scrollCopyTo(off int) {
	focused := a.wm.FocusedPane()
	if focused == nil {
		return
	}
	focused.Scroll(off)
	a.copyOffset = focused.ScrollOffset()
}

// exitCopyMode leaves copy mode and snaps the focused pane back to the live
// screen. It is safe to call when copy mode is not active.
func (a *App) exitCopyMode() {
	a.copyMode = false
	a.copyOffset = 0
	if focused := a.wm.FocusedPane(); focused != nil {
		focused.Scroll(0)
	}
}

// ScrollWheel scrolls the focused pane by delta lines in response to a mouse
// wheel event: positive moves toward the live screen (wheel down), negative
// into scrollback (wheel up). Scrolling into history arms copy mode so the view
// stays put until the next key; returning to the live screen disarms it. Run on
// the main goroutine (it mutates UI mode state).
func (a *App) ScrollWheel(delta int) {
	focused := a.wm.FocusedPane()
	if focused == nil {
		return
	}
	if !a.copyMode {
		a.copyMode = true
		a.copyOffset = 0
	}
	focused.Scroll(focused.ScrollOffset() - delta)
	a.copyOffset = focused.ScrollOffset()
	if a.copyOffset == 0 {
		a.copyMode = false
	}
}
