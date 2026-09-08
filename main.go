package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gdamore/tcell/v2"

	"github.com/atharva-0123-cs/tview-demo/layout"
	"github.com/atharva-0123-cs/tview-demo/pane"
)

var (
	wm           *layout.WindowManager
	screen       tcell.Screen
	prefixActive bool
	resizeMode   bool
	pasteActive  bool
	errorMsg     string
	shouldQuit   bool
)

// setErrorMessage records a status-bar message; it is cleared at the next key press.
func setErrorMessage(msg string) {
	errorMsg = msg
}

// handleKey processes prefix and resize commands.
// Returns true if the key event was consumed by the multiplexer.
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
func forwardKey(ev *tcell.EventKey) {
	if focused := wm.FocusedPane(); focused != nil {
		focused.HandleEvent(ev)
	}
}

// main opens the debug log, initializes the tcell screen and the window
// manager, creates the first pane, then runs the PollEvent loop until the
// last pane is closed.
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
