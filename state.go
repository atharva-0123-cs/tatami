// Package-level UI state (see main.go for the bootstrap, keys.go for the key
// handling). All of these variables are only read or written on the main
// goroutine inside the PollEvent loop (or by the render closure), so no locks
// are needed around them.
package main

import (
	"github.com/gdamore/tcell/v2"

	"github.com/atharva-0123-cs/tview-demo/layout"
)

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
