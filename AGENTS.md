# AGENTS.md

TUI "roge" terminal multiplexer built with `nsf/termbox-go` and `creack/pty`. Go module `github.com/atharva-0123-cs/tview-demo`. Writes debug log to `roge.log`.

## Build & run

- Build: `go build -o tview-demo .`
- Run: `./tview-demo` — requires a real TTY; exits 1 if stdin is not a TTY. Do not test with `</dev/null`.
- Binary `tview-demo` is gitignored; rebuild after every edit (`go build ./...` only type-checks, does not produce the binary).
- No tests, no CI, no linter/formatter config. `go vet ./...` is the only check.

## Architecture

- `main.go` — entrypoint. Initializes termbox, owns the Ctrl+A prefix and resize-mode state, and runs the `PollEvent` loop. `handleKey` consumes multiplexer commands; unconsumed keys are forwarded to the focused pane. A `render()` closure clears the termbox buffer and calls `wm.Draw` + `termbox.Flush` after every event.
- `pane/pane.go` — `Pane` wraps a `creack/pty` running a shell and a VT emulator (`pane/vt.go`) that buffers decoded escape sequences. One goroutine (`readPtyOutput`) per pane feeds shell output into the emulator under the pane mutex and wakes the main loop with `termbox.Interrupt()`. `Draw` copies the emulator grid into the termbox cell buffer; `KeyToBytes`/`HandleEvent` convert termbox events to the byte sequences the shell expects.
- `layout/layout.go` — `WindowManager` recurses over the `LayoutNode` tree to assign each pane a screen rectangle, calls each pane's `Draw`, and renders the status bar straight into the termbox buffer. Focus, zoom, split, resize, swap, and rotate mutate the tree; changes appear on the next `Draw` pass.

## Keybindings (Ctrl+A prefix)

c/-: h-split | C/|: v-split | h/j/k/l: focus | x: close | z: zoom | o: rotate | }/{: swap | r: resize mode (then h/j/k/l) | 1-9: jump to pane | q: quit

## Concurrency gotchas (critical)

- The termbox cell buffer may only be touched from the main goroutine: inside the `PollEvent` loop, in an event handler, or from `render()`. Background goroutines (e.g. `readPtyOutput`) must never call `termbox.SetCell`/`Flush`; they only mutate the emulator under the pane mutex and signal the main loop with `termbox.Interrupt()`.
- `termbox.Interrupt()` is safe to call from goroutines and wakes a blocked `PollEvent`. The initial pane is created by `main` before the loop, or lazily inside `wm.Draw` if the tree is empty.
- Keep the pane `Draw` call inside the manager's `Draw` so PTY `Winsize` resizes run on the main loop.
- Key handling runs on the main loop: `handleKey` returns `false` to forward an event to the focused pane's `HandleEvent`, which writes keystroke bytes to the PTY.
