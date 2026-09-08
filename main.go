// Command tview-demo is the entry point for the "roge" terminal
// multiplexer: it owns the tcell screen, the Ctrl+A prefix, the resize-mode
// state, and the PollEvent loop that drives every pane. The multiplexer key
// handling lives in keys.go and the per-loop UI state in state.go.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gdamore/tcell/v2"

	"github.com/atharva-0123-cs/tview-demo/config"
	"github.com/atharva-0123-cs/tview-demo/layout"
	"github.com/atharva-0123-cs/tview-demo/pane"
) // main opens the debug log, initializes the tcell screen and the window
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

	cfg := config.Load()
	log.Printf("config: prefix=%s mouse=%v", cfg.PrefixLabel, cfg.Mouse)

	screen, err = tcell.NewScreen()
	if err != nil {
		log.Fatalf("Error creating screen: %v", err)
	}
	if err := screen.Init(); err != nil {
		log.Fatalf("Error initializing screen: %v", err)
	}
	defer screen.Fini()

	screen.EnablePaste()
	if cfg.Mouse {
		screen.EnableMouse(tcell.MouseDragEvents)
	}

	// Wake the PollEvent loop whenever a background goroutine (PTY output
	// reader, process-exit waiter) has new data to show.
	pane.Interrupt = func() {
		_ = screen.PostEvent(tcell.NewEventInterrupt(nil))
	}

	wm = layout.NewWindowManager()
	if err := wm.NewPane(); err != nil {
		log.Fatalf("Error creating initial pane: %v", err)
	}

	style := layout.StatusStyle{PrefixLabel: cfg.PrefixLabel, Fg: cfg.StatusFg, Bg: cfg.StatusBg}

	render := func() {
		screen.Clear()
		w, h := screen.Size()
		wm.Draw(screen, w, h, prefixActive, resizeMode, errorMsg, style)
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
			} else if !handleKey(ev, cfg.PrefixKey) {
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
