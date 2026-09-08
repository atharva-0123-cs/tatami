package main

import (
	"fmt"
	"github.com/atharva-0123-cs/tview-demo/layout"
	"github.com/nsf/termbox-go"
	"log"
	"os"
)

var (
	wm           *layout.WindowManager
	prefixActive bool
	resizeMode   bool
	errorMsg     string
	shouldQuit   bool
)

// setErrorMessage records a status-bar message; it is cleared at the next key press.
func setErrorMessage(msg string) {
	errorMsg = msg
}

// handleKey processes prefix and resize commands.
// Returns true if the key event was consumed by the multiplexer.
func handleKey(ev termbox.Event) bool {
	// Escape cancels prefix and resize mode.
	if ev.Key == termbox.KeyEsc {
		if resizeMode || prefixActive {
			resizeMode = false
			prefixActive = false
			return true
		}
		return false // forward Esc to the shell
	}

	// Ctrl+A toggles prefix mode
	if ev.Key == termbox.KeyCtrlA {
		if resizeMode {
			resizeMode = false
		}
		prefixActive = !prefixActive
		return true
	}

	// Resize mode: h/j/k/l adjust the focused pane's proportions.
	if resizeMode {
		resizeMode = false
		ch := ev.Ch
		switch ch {
		case 'h':
			wm.DecreaseSize(layout.SplitVertical)
		case 'l':
			wm.IncreaseSize(layout.SplitVertical)
		case 'k':
			wm.DecreaseSize(layout.SplitHorizontal)
		case 'j':
			wm.IncreaseSize(layout.SplitHorizontal)
		}
		return true
	}

	// Prefix mode commands.
	if prefixActive {
		prefixActive = false
		ch := ev.Ch

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

		// Arrow keys during prefix mode - move focus.
		switch ev.Key {
		case termbox.KeyArrowLeft:
			wm.FocusLeft()
			return true
		case termbox.KeyArrowRight:
			wm.FocusRight()
			return true
		case termbox.KeyArrowUp:
			wm.FocusUp()
			return true
		case termbox.KeyArrowDown:
			wm.FocusDown()
			return true
		}

		// Any other key ends prefix mode without action.
		return true
	}

	// Default: pass the key through to the running shell.
	return false
}

// main opens the debug log, initializes termbox and the window manager, creates
// the first pane, then runs the PollEvent loop until the last pane is closed.
func main() {
	f, err := os.OpenFile("roge.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening log file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	log.SetOutput(f)
	log.Println("Starting roge TUI app...")

	if err := termbox.Init(); err != nil {
		log.Fatalf("Error initializing termbox: %v", err)
	}
	defer termbox.Close()

	termbox.SetOutputMode(termbox.Output256)

	wm = layout.NewWindowManager()
	if err := wm.NewPane(); err != nil {
		log.Fatalf("Error creating initial pane: %v", err)
	}

	render := func() {
		termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
		w, h := termbox.Size()
		wm.Draw(w, h, prefixActive, resizeMode, errorMsg)
		termbox.Flush()
	}

	render()

	for !shouldQuit {
		ev := termbox.PollEvent()

		// Auto-close panes whose shell exited (e.g. `exit`); if the last
		// pane closed this way, quit the app.
		if wm.CloseDeadPanes() {
			shouldQuit = true
		}

		switch ev.Type {
		case termbox.EventKey:
			errorMsg = ""

			if !handleKey(ev) {
				focused := wm.FocusedPane()
				if focused != nil {
					focused.HandleEvent(ev)
				}
			}
			render()

		case termbox.EventResize:
			render()

		case termbox.EventInterrupt:
			render()

		case termbox.EventError:
			log.Printf("Termbox error event: %v", ev.Err)
		}
	}

	for _, p := range wm.CollectPanes() {
		_ = p.Close()
	}

	log.Println("Exiting roge TUI app.")
}
