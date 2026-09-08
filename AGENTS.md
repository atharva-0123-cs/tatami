# AGENTS.md

TUI "roge" terminal multiplexer built with `gdamore/tcell/v2` and `creack/pty`. Go module `github.com/atharva-0123-cs/tview-demo`. Writes debug log to `roge.log`.

## Build & run

- Build: `go build -o tview-demo .`
- Run: `./tview-demo` — requires a real TTY; exits 1 if stdin is not a TTY. Do not test with `</dev/null`.
- Binary `tview-demo` is gitignored; rebuild after every edit (`go build ./...` only type-checks, does not produce the binary).
- No tests, no CI, no linter/formatter config. `go vet ./...` + `go test ./...` are the checks.

## Architecture

- `main.go` — entrypoint. Creates + initializes the tcell screen (enables bracketed paste), installs `pane.Interrupt`, owns the Ctrl+A prefix and resize-mode state, and runs the `PollEvent` loop. `handleKey` consumes multiplexer commands; unconsumed keys are forwarded to the focused pane. A `render()` closure clears the screen, calls `wm.Draw`, and calls `screen.Show()` after every event.
- `pane/pane.go` — `Pane` wraps a `creack/pty` running a shell and a VT emulator (`pane/vt.go`) that buffers decoded escape sequences. One goroutine (`readPtyOutput`) per pane feeds shell output into the emulator under the pane mutex and wakes the main loop via the `pane.Interrupt` hook (which posts an `EventInterrupt`). `Draw` copies the emulator grid into the screen; `KeyToBytes`/`HandleEvent` convert tcell events to the byte sequences the shell expects.
- `pane/vt.go` — pure emulator, no UI dependency. Cells store a `vtColor` (default / palette index / truecolor RGB). Tracks DECTCEM cursor visibility, DECCKM app cursor keys, and DECSET 2004 bracketed paste.
- `layout/layout.go` — `WindowManager` recurses over the `LayoutNode` tree to assign each pane a screen rectangle, calls each pane's `Draw`, and renders the status bar straight into the screen buffer. Focus, zoom, split, resize, swap, and rotate mutate the tree; changes appear on the next `Draw` pass.

## Keybindings (Ctrl+A prefix)

c/-: h-split | C/|: v-split | h/j/k/l: focus | x: close | z: zoom | o: rotate | }/{: swap | r: resize mode (then h/j/k/l) | 1-9: jump to pane | q: quit

## Concurrency gotchas (critical)

- The tcell screen buffer may only be touched from the main goroutine: inside the `PollEvent` loop, in an event handler, or from `render()`. Background goroutines (e.g. `readPtyOutput`) must never call `SetContent`/`Show`; they only mutate the emulator under the pane mutex and signal the main loop via `pane.Interrupt` → `screen.PostEvent(NewEventInterrupt(nil))`.
- `PostEvent` is non-blocking: if the event queue is full the wake-up is dropped. That is safe — the main loop is already processing a queue of pending events, so a render is imminent.
- The initial pane is created by `main` before the loop, or lazily inside `wm.Draw` if the tree is empty.
- Keep the pane `Draw` call inside the manager's `Draw` so PTY `Winsize` resizes run on the main loop.
- Key handling runs on the main loop: `handleKey` returns `false` to forward an event to the focused pane's `HandleEvent`, which writes keystroke bytes to the PTY.
- While a bracketed paste is active (`pasteActive`), every `EventKey` is forwarded verbatim to the focused pane; prefix/command parsing is bypassed so pasted text can never trigger a multiplexer command.