package main

import (
	"log"
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/atharva-0123-cs/tview-demo/layout"
)

var (
	app          *tview.Application
	wm           *layout.WindowManager
	prefixActive bool
	statusBar    *tview.TextView
)

func updateStatusBar() {
	if prefixActive {
		statusBar.SetText(" [yellow][PREFIX ACTIVE][white]  Press: [green]c[white] (new pane) | [green]x[white] (close) | [green]←/→[white] or [green]h/l[white] (focus) | [green]q[white] (quit)")
	} else {
		statusBar.SetText(" [green]ROGE[white] | Prefix: [yellow]Ctrl+A[white] | [dim]Ctrl+A then c/x/h/l/q[white]")
	}
}

func main() {
	// Setup logging to a file for debugging
	f, err := os.OpenFile("roge.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)
	log.Println("Starting roge TUI app...")

	app = tview.NewApplication()
	wm = layout.NewWindowManager(app)

	// Status bar at the bottom
	statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	updateStatusBar()

	// Main root layout: WindowManager flex on top, status bar on bottom
	rootFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(wm.GetRootLayout(), 0, 1, true).
		AddItem(statusBar, 1, 1, false)

	// Add initial pane
	if err := wm.NewPane(); err != nil {
		log.Fatalf("Failed to create initial pane: %v", err)
	}

	// Global input capture for prefix key and pane management
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Handle Prefix Key activation: Ctrl+A
		if event.Key() == tcell.KeyCtrlA {
			prefixActive = !prefixActive
			updateStatusBar()
			return nil
		}

		if prefixActive {
			prefixActive = false
			updateStatusBar()

			switch event.Rune() {
			case 'c': // New pane
				if err := wm.NewPane(); err != nil {
					log.Printf("Error creating new pane: %v", err)
				}
				return nil
			case 'x': // Close pane
				wm.CloseCurrentPane()
				return nil
			case 'h': // Focus left
				wm.MoveFocusLeft()
				return nil
			case 'l': // Focus right
				wm.MoveFocusRight()
				return nil
			case 'q': // Quit
				app.Stop()
				return nil
			}

			// Arrow keys during prefix mode
			switch event.Key() {
			case tcell.KeyLeft:
				wm.MoveFocusLeft()
				return nil
			case tcell.KeyRight:
				wm.MoveFocusRight()
				return nil
			}
		}

		return event
	})

	if err := app.SetRoot(rootFlex, true).EnableMouse(true).Run(); err != nil {
		log.Fatalf("Error running application: %v", err)
	}
	log.Println("Exiting roge TUI app.")
}
