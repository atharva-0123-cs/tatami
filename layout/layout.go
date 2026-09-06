package layout

import (
	"fmt"
	"log"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/atharva-0123-cs/tview-demo/pane"
)

// WindowManager manages the panes and their layout
type WindowManager struct {
	app       *tview.Application
	flex      *tview.Flex
	panes     []*pane.Pane
	currentID int // For unique pane labels
}

// NewWindowManager creates a new WindowManager
func NewWindowManager(app *tview.Application) *WindowManager {
	wm := &WindowManager{
		app:       app,
		flex:      tview.NewFlex(),
		panes:     make([]*pane.Pane, 0),
		currentID: 0,
	}
	// Initially, set the flex to have some default styling
	wm.flex.SetInputCapture(wm.handleLayoutInput)
	return wm
}

// GetRootLayout returns the main tview.Primitive for the layout
func (wm *WindowManager) GetRootLayout() tview.Primitive {
	return wm.flex
}

// NewPane adds a new terminal pane to the layout
func (wm *WindowManager) NewPane() error {
	label := fmt.Sprintf("pane-%d", wm.currentID)
	pane, err := pane.NewPane(wm.app, label)
	if err != nil {
		log.Printf("Failed to create new pane: %v", err)
		return err
	}

	wm.panes = append(wm.panes, pane)
	wm.updateLayout()
	wm.currentID++
	wm.app.SetFocus(pane) // Set focus to the newly created pane
	return nil
}

// CloseCurrentPane removes the currently focused pane
func (wm *WindowManager) CloseCurrentPane() {
	if len(wm.panes) == 0 {
		return
	}

	currentPane := wm.app.GetFocus().(tview.Primitive)

	// Find the index of the current pane in our list
	var paneToRemove *pane.Pane
	var currentIndex int

	for i, p := range wm.panes {
		if p == currentPane {
			paneToRemove = p
			currentIndex = i
			break
		}
	}

	if paneToRemove != nil {
		// Close the pty and process
		if err := paneToRemove.Close(); err != nil {
			log.Printf("Error closing pane %s: %v", paneToRemove.GetTitle(), err)
		}

		// Remove from slice
		wm.panes = append(wm.panes[:currentIndex], wm.panes[currentIndex+1:]...)
		wm.updateLayout()

		// Set focus to the previous or next pane if available
		newFocusIndex := currentIndex - 1
		if newFocusIndex < 0 && len(wm.panes) > 0 {
			newFocusIndex = 0
		}
		if len(wm.panes) > 0 {
			wm.app.SetFocus(wm.panes[newFocusIndex])
		} else {
			// If no panes left, maybe exit or show a message
			wm.app.Stop()
		}
	}
}

// MoveFocusLeft attempts to move focus to the pane on the left
func (wm *WindowManager) MoveFocusLeft() {
	wm.changeFocus(-1) // -1 for left
}

// MoveFocusRight attempts to move focus to the pane on the right
func (wm *WindowManager) MoveFocusRight() {
	wm.changeFocus(1) // 1 for right
}

// changeFocus moves focus to an adjacent pane based on direction
func (wm *WindowManager) changeFocus(direction int) {
	currentPrimitive := wm.app.GetFocus()
	var currentIndex int = -1

	for i, p := range wm.panes {
		if p == currentPrimitive {
			currentIndex = i
			break
		}
	}

	if currentIndex == -1 {
		// If focus is not on a pane, maybe focus the first one
		if len(wm.panes) > 0 {
			wm.app.SetFocus(wm.panes[0])
		}
		return
	}

	// Simple linear navigation: move left/right in the slice
	nextIndex := currentIndex + direction

	// Wrap around or clamp to bounds
	if nextIndex < 0 {
		nextIndex = len(wm.panes) - 1
	} else if nextIndex >= len(wm.panes) {
		nextIndex = 0
	}

	if len(wm.panes) > 0 {
		wm.app.SetFocus(wm.panes[nextIndex])
	}
}

// updateLayout rebuilds the Flex layout with current panes
func (wm *WindowManager) updateLayout() {
	wm.flex.Clear() // Remove all current items
	for _, p := range wm.panes {
		wm.flex.AddItem(p, 0, 1, true) // Add each pane with flexible size
	}
	// Ensure the current focus remains on a valid pane after layout change
	wm.ensureFocus()
}

// ensureFocus makes sure the focus is set to a valid pane if it was lost
func (wm *WindowManager) ensureFocus() {
	currentFocus := wm.app.GetFocus()
	if currentFocus == nil {
		// If no focus, set it to the first pane
		if len(wm.panes) > 0 {
			wm.app.SetFocus(wm.panes[0])
		}
		return
	}

	// Check if the current focus is still a valid primitive in our list
	isValidFocus := false
	for _, p := range wm.panes {
		if p == currentFocus {
			isValidFocus = true
			break
		}
	}

	// If focus was lost (e.g., pane removed), focus the first available pane
	if !isValidFocus && len(wm.panes) > 0 {
		wm.app.SetFocus(wm.panes[0])
	}
}

// handleLayoutInput is for keybindings specific to the layout manager itself (e.g. switching panes).
// In this simple case, most pane switching is handled by main.go's global capture.
// We can expand this for more complex layout operations like splitting panes or moving them.
func (wm *WindowManager) handleLayoutInput(event *tcell.EventKey) *tcell.EventKey {
	// For now, we delegate most pane management to main.go.
	// If we wanted specific layout-only commands, they would go here.
	// For example, a command to split the current pane vertically/horizontally.
	return event // Let the child pane handle its own input if it has focus
}
