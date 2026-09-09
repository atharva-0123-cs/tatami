# AGENTS.md

TUI "tatami" terminal multiplexer built with `gdamore/tcell/v2` and `creack/pty`. Go module `github.com/atharva-0123-cs/tatami`. Writes debug log to `tatami.log`.

## Build & run

- Build: `go build -o tatami .`
- Run: `./tatami` — requires a real TTY; exits 1 if stdin is not a TTY. Do not test with `</dev/null`.
- Binary `tatami` is gitignored; rebuild after every edit (`go build ./...` only type-checks, does not produce the binary).
- No tests, no CI, no linter/formatter config. `go vet ./...` + `go test ./...` are the checks.

## Architecture

- `main.go` — entrypoint. Creates + initializes the tcell screen (enables bracketed paste, and mouse when `mouse=on`) and runs the `PollEvent` loop. Bootstraps the structured logger via `setupLogger` (two-phase: defaults then the loaded profile). `keys.go` handles the multiplexer commands (`handleKey`); unconsumed keys are forwarded to the focused pane. Per-loop UI state (prefix/resize/paste modes) lives in `state.go`. A `render()` closure clears the screen, calls `wm.Draw`, and calls `screen.Show()` after every event.
- `config/` — `tatami.conf` loader (`KEY=value`, `#` comments) with `TATAMI_<NAME>` env overrides: prefix key, mouse toggle, status-bar colors, log level/format, pane gap.
- `format/` — custom `slog.Handler` rendering each record as `[LEVEL] time file:line : message key=value...` (the `text` format; `json` uses the stdlib JSON handler).
- `pane/pane.go` — `Pane` type + lifecycle: wraps a `creack/pty` running a shell, owns the emulator, spawns the output-reader and process-wait goroutines. `process.go` selects the shell (`shellCandidates`) and starts it; `output.go` (`readPtyOutput`) feeds shell output into the emulator under the pane mutex and wakes the main loop via the `pane.Interrupt` hook (which posts an `EventInterrupt`); `draw.go` paints the border and emulator grid into the screen; `input.go` converts tcell events to the byte sequences the shell expects (`KeyToBytes`/`HandleEvent`) plus clipboard/paste helpers; `clipboard.go` copies text to the system clipboard.
- `pane/vt.go` — pure emulator, no UI dependency. Cells store a `vtColor` (default / palette index / truecolor RGB). Tracks DECTCEM cursor visibility, DECCKM app cursor keys, and DECSET 2004 bracketed paste.
- `layout/layout.go` — `WindowManager` types and tree primitives over the `LayoutNode` tree. Multiple windows (workspaces) are supported: each window owns a layout tree, and the active window's state (root/focused/zoomed/paneCount) lives directly on the `WindowManager`; the `windows` slice holds one `Window` per window whose entry at `currentWin` mirrors the active state and is refreshed on every switch (`saveCurrent`/`switchTo`). `draw.go` recurses to assign each pane a screen rectangle (insetting by the configurable pane gap — `SetPaneGap`/`gapGeometry` derive a per-side inset + shared-edge overlap from the `pane_gap` setting so the visible gutter equals the configured cell count), calls each pane's `Draw`, and renders the status bar; `splits.go` creates panes, `close.go` removes them (via `windowGone` an emptied window is dropped and a neighbor adopted; `CloseDeadPanes` sweeps all windows), `focus.go` moves focus, `transform.go` zooms/swaps/rotates, `resize.go` adjusts proportions. Changes appear on the next `Draw` pass.

## Keybindings (default Ctrl+A prefix; configurable via `tatami.conf` `prefix =`)

c/-: h-split | C/|: v-split | n: new window | w: next win | W/p: prev win | h/j/k/l: focus | x: close | z: zoom | o: rotate | }/{: swap | r: resize mode (then h/j/k/l) | 1-9: jump to pane | q: quit

## Concurrency gotchas (critical)

- The tcell screen buffer may only be touched from the main goroutine: inside the `PollEvent` loop, in an event handler, or from `render()`. Background goroutines (e.g. `readPtyOutput`) must never call `SetContent`/`Show`; they only mutate the emulator under the pane mutex and signal the main loop via `pane.Interrupt` → `screen.PostEvent(NewEventInterrupt(nil))`.
- `PostEvent` is non-blocking: if the event queue is full the wake-up is dropped. That is safe — the main loop is already processing a queue of pending events, so a render is imminent.
- The initial pane is created by `main` before the loop, or lazily inside `wm.Draw` if the tree is empty.
- Keep the pane `Draw` call inside the manager's `Draw` so PTY `Winsize` resizes run on the main loop.
- Key handling runs on the main loop: `handleKey` returns `false` to forward an event to the focused pane's `HandleEvent`, which writes keystroke bytes to the PTY.
- While a bracketed paste is active (`pasteActive`), every `EventKey` is forwarded verbatim to the focused pane; prefix/command parsing is bypassed so pasted text can never trigger a multiplexer command.