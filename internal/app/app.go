// Package app assembles the components of the multiplexer into a single App
// and runs the tcell event loop.
//
// The App owns everything the UI touches on the main goroutine: the tcell
// screen, the layout WindowManager, and the per-loop UI state (prefix/resize/
// copy/paste modes, scroll offset, transient status message, quit flag). The
// previous package-level globals for this state were moved here so a second
// run in one process is possible and the startup/teardown wiring is explicit.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/gdamore/tcell/v2"

	"github.com/atharva-0123-cs/tatami/config"
	"github.com/atharva-0123-cs/tatami/layout"
	"github.com/atharva-0123-cs/tatami/pane"
)

// App holds the running multiplexer: the tcell screen, the layout tree, and
// the per-loop UI state that used to be package-level variables.
//
// All fields are only read or written on the main goroutine inside Run (or by
// render), so no locks are needed.
type App struct {
	screen     tcell.Screen
	wm         *layout.WindowManager
	prefixKey  tcell.Key
	style      layout.StatusStyle

	prefixActive bool // prefix mode is armed
	resizeMode   bool // resize mode (prefix+r) is armed
	pasteActive  bool // a bracketed paste is in progress
	copyMode     bool // copy/scroll mode is armed
	copyOffset   int  // scroll offset in copy mode (lines from bottom)
	errorMsg     string
	shouldQuit   bool

	// FalkorDB graph prompt state (see graph.go).
	graphMode   graphModeKind
	graphBuf    string
	graphResult string
	graphScroll int

	graphCancel context.CancelFunc // stops the background FalkorDB health monitor
}

// New creates the screen, starts the layout with the configured pane gap and
// an initial pane, and installs the background-goroutine wake-up hook.
//
// The caller is responsible for calling Cleanup (even on error) to restore the
// terminal; screen.Init may already have switched the real terminal's modes.
func New(cfg *config.Config) (*App, error) {
	a := &App{
		prefixKey: cfg.PrefixKey,
		style: layout.StatusStyle{
			PrefixLabel: cfg.PrefixLabel,
			Fg:          cfg.StatusFg,
			Bg:          cfg.StatusBg,
		},
	}

	var err error
	a.screen, err = tcell.NewScreen()
	if err != nil {
		return nil, fmt.Errorf("error creating screen: %w", err)
	}
	if err = a.screen.Init(); err != nil {
		return nil, fmt.Errorf("error initializing screen: %w", err)
	}
	if cfg.Mouse {
		a.screen.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents)
	}

	// Wake the PollEvent loop whenever a background goroutine (PTY output
	// reader, process-exit waiter) has new data to show.
	pane.Interrupt = func() {
		_ = a.screen.PostEvent(tcell.NewEventInterrupt(nil))
	}

	a.wm = layout.NewWindowManager()
	a.wm.SetPaneGap(cfg.PaneGap)
	if err := a.wm.NewPane(); err != nil {
		a.Cleanup()
		return nil, fmt.Errorf("error creating initial pane: %w", err)
	}

	return a, nil
}

// Render draws the whole screen: clears it, draws the layout and status bar,
// and flushes. Must only be called on the main goroutine.
func (a *App) Render() {
	a.screen.Clear()
	w, h := a.screen.Size()
	a.wm.SetStatusExtra(a.graphStatusLabel())
	a.wm.Draw(a.screen, w, h, a.prefixActive, a.resizeMode, a.copyMode, a.errorMsg, a.style)
	a.renderGraphOverlay()
	a.screen.Show()
}

// SetErrorMsg records a single status-bar message. It is shown on the next
// render and stays until the user presses any key (Run clears errorMsg at the
// start of every EventKey dispatch).
func (a *App) SetErrorMsg(msg string) { a.errorMsg = msg }

// Run is the main event loop. It returns when quit is requested (prefix+q, the
// last pane closed, or the final dead pane swept).
func (a *App) Run() {
	a.Render()
	a.startGraphMonitor()

	for !a.shouldQuit {
		ev := a.screen.PollEvent()

		// Auto-close panes whose shell exited (e.g. `exit`); if the last
		// pane closed this way, quit the app.
		if a.wm.CloseDeadPanes() {
			a.shouldQuit = true
		}

		switch ev := ev.(type) {
		case *tcell.EventKey:
			a.errorMsg = ""
			if a.pasteActive {
				// While bracketed paste is active, raw content is forwarded
				// verbatim so pasted text can never be mistaken for prefix
				// commands (Ctrl+A, q, x, ...).
				a.forwardKey(ev)
			} else if !a.handleKey(ev) {
				a.forwardKey(ev)
			}
			a.Render()

		case *tcell.EventPaste:
			if ev.Start() {
				a.pasteActive = true
				if focused := a.wm.FocusedPane(); focused != nil && focused.Paste() {
					_, _ = focused.WriteInput([]byte("\x1b[200~"))
				}
			} else {
				a.pasteActive = false
				if focused := a.wm.FocusedPane(); focused != nil && focused.Paste() {
					_, _ = focused.WriteInput([]byte("\x1b[201~"))
				}
			}
			a.Render()

		case *tcell.EventMouse:
			buttons := ev.Buttons()
			if buttons&tcell.ButtonPrimary != 0 {
				px, py := ev.Position()
				sw, sh := a.screen.Size()
				if a.wm.FocusAt(px, py, sw, sh) {
					a.Render()
				}
			}

		case *tcell.EventResize:
			a.Render()

		case *tcell.EventInterrupt:
			a.Render()

		case *tcell.EventError:
			slog.Error("screen event error", "err", ev.Error())
		}
	}
}

// Cleanup terminates every pane's process, finalizes the tcell screen (if it
// was initialized), and restores the terminal to its original state. It is
// safe to call multiple times.
func (a *App) Cleanup() {
	if a.graphCancel != nil {
		a.graphCancel()
		a.graphCancel = nil
	}
	if a.wm != nil {
		for _, p := range a.wm.CollectAllPanes() {
			_ = p.Close()
		}
		a.wm = nil
	}
	if a.screen != nil {
		a.screen.Fini()
		a.screen = nil
	}
	// tcell's restore is terminfo-dependent and on some terminals leaves the
	// alternate screen active with raw cursor/character modes. Emit the standard
	// xterm reset sequences directly to stdout so the shell always regains a
	// clean, interactive terminal regardless of the terminfo entry used.
	fmt.Fprint(os.Stdout, "\x1b[0m\x1b[39;49m\x1b[?25h\x1b[?1l\x1b[?1049l")
}