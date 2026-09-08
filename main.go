// Command tview-demo is the entry point for the "roge" terminal
// multiplexer: it owns the tcell screen, the Ctrl+A prefix, the resize-mode
// state, and the PollEvent loop that drives every pane.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gdamore/tcell/v2"

	"github.com/atharva-0123-cs/tview-demo/layout"
	"github.com/atharva-0123-cs/tview-demo/pane"
)

// Package-level UI state. All of these are only read or written on the main
// goroutine inside the PollEvent loop (or by the render closure), so no locks
// are needed around them.
var (
	wm           *layout.WindowManager // the layout tree plus focus/bookkeeping
	screen       tcell.Screen          // the single tcell screen (one per process)
	prefixActive bool                  // Ctrl+A prefix mode is armed
	resizeMode   bool                  // resize mode (Ctrl+A r) is armed
	pasteActive  bool                  // a bracketed paste is in progress
	errorMsg     string                // transient status-bar message, cleared on the next key
	shouldQuit   bool                  // when true the PollEvent loop exits
)

// setErrorMessage records a single status-bar message. The message is shown in
// the status bar on the next render and stays until the user presses any key:
// the PollEvent loop clears errorMsg at the start of every EventKey dispatch.
func setErrorMessage(msg string) {
	errorMsg = msg
}

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
//     simply ends prefix mode without an action, mirroring the historical
//     termbox behavior where modified keys had Ch == 0.
//  5. Otherwise the event is forwarded to the shell.
func handleKey(ev *tcell.EventKey) bool {
	// Escape cancels prefix and resize mode.
	if ev.Key() == tcell.KeyEsc {
		if resizeMode || prefixActive {
			resizeMode = false
			prefixActive = false
			return true
		}
		return false // forward Esc to the shell
	}

	// Ctrl+A toggles prefix mode
	if ev.Key() == tcell.KeyCtrlA {
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

// main opens the debug log, initializes the tcell screen and the window
// manager, creates the first pane, then runs the PollEvent loop until the
// last pane is closed.
//
// The loop wakes on any event type: keys are routed through handleKey/forwardKey
// (bracketed paste forwards verbarim), paste start/end marks pasteActive and
// emits the bracketed-paste delimiters when the shell enabled mode 2004,
// resize/interrupt events trigger a render, and error events are only logged.
// Renders always go through the render closure (Clear -> wm.Draw -> Show),
// which must stay on this goroutine so the tcell screen buffer is only ever
// touched from the main thread.
func main() {
	f, err := os.OpenFile("roge.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening log file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	log.SetOutput(f)
	log.Println("Starting roge TUI app...")

	screen, err = tcell.NewScreen()
	if err != nil {
		log.Fatalf("Error creating screen: %v", err)
	}
	if err := screen.Init(); err != nil {
		log.Fatalf("Error initializing screen: %v", err)
	}
	defer screen.Fini()

	screen.EnablePaste()

	// Wake the PollEvent loop whenever a background goroutine (PTY output
	// reader, process-exit waiter) has new data to show.
	pane.Interrupt = func() {
		_ = screen.PostEvent(tcell.NewEventInterrupt(nil))
	}

	wm = layout.NewWindowManager()
	if err := wm.NewPane(); err != nil {
		log.Fatalf("Error creating initial pane: %v", err)
	}

	render := func() {
		screen.Clear()
		w, h := screen.Size()
		wm.Draw(screen, w, h, prefixActive, resizeMode, errorMsg)
		screen.Show()
	}

	render()

	for !shouldQuit {
		ev := screen.PollEvent()

		// Auto-close panes whose shell exited (e.g. `exit`); if the last
		// pane closed this way, quit the app.
		if wm.CloseDeadPanes() {
			shouldQuit = true
		}

		switch ev := ev.(type) {
		case *tcell.EventKey:
			errorMsg = ""
			if pasteActive {
				// While bracketed paste is active, raw content is forwarded
				// verbatim so pasted text can never be mistaken for prefix
				// commands (Ctrl+A, q, x, ...).
				forwardKey(ev)
			} else if !handleKey(ev) {
				forwardKey(ev)
			}
			render()

		case *tcell.EventPaste:
			if ev.Start() {
				pasteActive = true
				if focused := wm.FocusedPane(); focused != nil && focused.Paste() {
					_, _ = focused.WriteInput([]byte("\x1b[200~"))
				}
			} else {
				pasteActive = false
				if focused := wm.FocusedPane(); focused != nil && focused.Paste() {
					_, _ = focused.WriteInput([]byte("\x1b[201~"))
				}
			}
			render()

		case *tcell.EventResize:
			render()

		case *tcell.EventInterrupt:
			render()

		case *tcell.EventError:
			log.Printf("Screen error event: %v", ev.Error())
		}
	}

	for _, p := range wm.CollectPanes() {
		_ = p.Close()
	}

	log.Println("Exiting roge TUI app.")
}
