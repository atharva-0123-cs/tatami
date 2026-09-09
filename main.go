package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/gdamore/tcell/v2"

	"github.com/atharva-0123-cs/tatami/config"
	"github.com/atharva-0123-cs/tatami/format"
	"github.com/atharva-0123-cs/tatami/layout"
	"github.com/atharva-0123-cs/tatami/pane"
)

func openLogFile() *os.File {
	f, err := os.OpenFile("tatami.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tatami: cannot open log file: %v\n", err)
		os.Exit(1)
	}
	return f
}

// ("text" renders "[INFO] time file:line : message", "json" is for tooling)
func setupLogger(f *os.File, level slog.Level, kind string) {
	opts := &slog.HandlerOptions{Level: level, AddSource: true}
	var h slog.Handler
	if kind == "text" {
		h = format.NewConsoleHandler(f, opts)
	} else {
		h = slog.NewJSONHandler(f, opts)
	}
	slog.SetDefault(slog.New(h))
}

// fail logs a fatal startup error (to the log file) and mirrors it on stderr
// before exiting with status 1.
func fail(msg string, err error) {
	if err != nil {
		msg = fmt.Sprintf("%s: %v", msg, err)
	}
	slog.Error(msg)
	fmt.Fprintf(os.Stderr, "tatami: %s\n", msg)
	cleanup()
	os.Exit(1)
}

func main() {
	f := openLogFile()
	defer f.Close()

	// Initial logger with defaults so any config warnings are captured in the
	// log file; recreated below once the effective profile is known.
	setupLogger(f, slog.LevelInfo, "text")

	cfg := config.Load()
	setupLogger(f, cfg.LogLevel, cfg.LogFormat)

	slog.Info("tatami starting",
		"prefix", cfg.PrefixLabel,
		"mouse", cfg.Mouse,
		"pane_gap", cfg.PaneGap,
	)

	// Defer cleanup until the very end, so panics restore the terminal too.
	defer cleanup()

	var err error
	screen, err = tcell.NewScreen() // assign to package-level var (never shadow it)
	if err != nil {
		fail("error creating screen", err)
	}
	if err = screen.Init(); err != nil {
		fail("error initializing screen", err)
	}
	if cfg.Mouse {
		screen.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents)
	}

	// Wake the PollEvent loop whenever a background goroutine (PTY output
	// reader, process-exit waiter) has new data to show.
	pane.Interrupt = func() {
		_ = screen.PostEvent(tcell.NewEventInterrupt(nil))
	}

	wm = layout.NewWindowManager()
	wm.SetPaneGap(cfg.PaneGap)
	if err := wm.NewPane(); err != nil {
		fail("error creating initial pane", err)
	}

	style := layout.StatusStyle{PrefixLabel: cfg.PrefixLabel, Fg: cfg.StatusFg, Bg: cfg.StatusBg}

	render := func() {
		screen.Clear()
		w, h := screen.Size()
		wm.Draw(screen, w, h, prefixActive, resizeMode, copyMode, errorMsg, style)
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

		case *tcell.EventMouse:
			buttons := ev.Buttons()
			if buttons&tcell.ButtonPrimary != 0 {
				px, py := ev.Position()
				sw, sh := screen.Size()
				if wm.FocusAt(px, py, sw, sh) {
					render()
				}
			}

		case *tcell.EventResize:
			render()

		case *tcell.EventInterrupt:
			render()

		case *tcell.EventError:
			slog.Error("screen event error", "err", ev.Error())
		}
	}

}

// cleanup terminates every pane's process, finalizes the tcell screen (if it
// was initialized), and restores the terminal to its original state. It is safe
// to call multiple times.
func cleanup() {
	if wm != nil {
		for _, p := range wm.CollectAllPanes() {
			_ = p.Close()
		}
		wm = nil
	}
	if screen != nil {
		screen.Fini()
		screen = nil
	}
	// tcell's restore is terminfo-dependent and on some terminals leaves the
	// alternate screen active with raw cursor/character modes. Emit the standard
	// xterm reset sequences directly to stdout so the shell always regains a
	// clean, interactive terminal regardless of the terminfo entry used.
	fmt.Fprint(os.Stdout, "\x1b[0m\x1b[39;49m\x1b[?25h\x1b[?1l\x1b[?1049l")
}
