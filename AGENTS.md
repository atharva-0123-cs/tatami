# AGENTS.md

TUI "tatami" terminal multiplexer built with `gdamore/tcell/v2` and `creack/pty`. Go module `github.com/atharva-0123-cs/tatami`. Writes debug log to `tatami.log`.

## Build & run

- Build: `go build -o tatami .`
- Run: `./tatami` — requires a real TTY; exits 1 if stdin is not a TTY. Do not test with `</dev/null`.
- Binary `tatami` is gitignored; rebuild after every edit (`go build ./...` only type-checks, does not produce the binary).
- No tests, no CI, no linter/formatter config. `go vet ./...` + `go test ./...` are the checks.

## Architecture

- `main.go` — thin launcher: opens `tatami.log`, bootstraps the structured logger via `setupLogger` (two-phase: defaults then the loaded profile), loads the config, then `app.New(cfg)` + `a.Run()`. All screen/loop/key logic lives in `internal/app`.
- `internal/app/` — `App` struct (screen, `*layout.WindowManager`, and the per-loop UI state that used to be package globals in `state.go`: prefix/resize/paste/copy modes, scroll offset, error message, quit flag). `app.go` creates/initializes the tcell screen (paste + mouse when `mouse=on`), installs the `pane.Interrupt` wake-up hook, and runs the `PollEvent` loop (`Run`); `keys.go` is the multiplexer command parser (`handleKey`) with unconsumed keys forwarded to the focused pane; `Cleanup` closes every pane, finishes the screen, and emits xterm reset sequences.
- `config/` — `tatami.conf` loader (`KEY=value`, `#` comments) with `TATAMI_<NAME>` env overrides: prefix key, mouse toggle, status-bar colors, log level/format, pane gap.
- `format/` — custom `slog.Handler` rendering each record as `[LEVEL] time file:line : message key=value...` (the `text` format; `json` uses the stdlib JSON handler).
- `term/` — pure VT100/xterm emulator, no UI/PTY dependency. `terminal.go` holds the `Terminal` type + grid (cells carry a `term.Color`: default / palette index / truecolor RGB) + feed/cursor/scroll primitives, `csi.go` parses ESC/CSI/OSC/modes, `sgr.go` applies colors/attributes, `scrollback.go` does scrollback, the copy-mode view, and text extraction. Tracks DECTCEM cursor visibility, DECCKM app cursor keys, and DECSET 2004 bracketed paste. White-box tests in `term/terminal_test.go`.
- `pane/pane.go` — `Pane` type + lifecycle: wraps a `creack/pty` running a shell, owns a `*term.Terminal`, spawns the output-reader and process-wait goroutines. `process.go` selects the shell (`shellCandidates`) and starts it; `output.go` (`readPtyOutput`) feeds shell output into the emulator under the pane mutex and wakes the main loop via the `pane.Interrupt` hook (which posts an `EventInterrupt`); `draw.go` paints the border and emulator grid into the screen; `input.go` converts tcell events to the byte sequences the shell expects (`KeyToBytes`/`HandleEvent`) plus clipboard/paste helpers; `clipboard.go` copies text to the system clipboard.
- `window/` — workspace model. `tree.go` has the layout tree primitives (`window.Node`, `SplitDir`/`SplitHorizontal`/`SplitVertical`) with exported `Children`/`Sizes`/`Parent` fields; `window.go` is the `Window` save slot (name/root/focused/paneCount/zoomed); `registry.go` is the `Registry` list of windows with the active-window index/name.
- `layout/layout.go` — `WindowManager` (active-window state root/focused/paneCount/zoomed lives directly on it) embedding a `window.Registry`; the `windows` slice entry at `CurrentWin` mirrors the active state and is refreshed on every switch (`saveCurrent`/`switchTo`). `draw.go` recurses to assign each pane a screen rectangle (insetting by the configurable pane gap — `SetPaneGap`/`gapGeometry` derive a per-side inset + shared-edge overlap from the `pane_gap` setting so the visible gutter equals the configured cell count), calls each pane's `Draw`, and renders the status bar; `splits.go` creates panes, `close.go` removes them (via `windowGone` an emptied window is dropped and a neighbor adopted; `CloseDeadPanes` sweeps all windows), `focus.go` moves focus, `transform.go` zooms/swaps/rotates, `resize.go` adjusts proportions. Changes appear on the next `Draw` pass.

## Keybindings (default Ctrl+A prefix; configurable via `tatami.conf` `prefix =`)

c/-: h-split | C/|: v-split | n: new window | w: next win | W/p: prev win | h/j/k/l: focus | x: close | z: zoom | o: rotate | }/{: swap | r: resize mode (then h/j/k/l) | 1-9: jump to pane | q: quit

## Concurrency gotchas (critical)

- The tcell screen buffer may only be touched from the main goroutine: inside the `PollEvent` loop, in an event handler, or from `render()`. Background goroutines (e.g. `readPtyOutput`) must never call `SetContent`/`Show`; they only mutate the emulator under the pane mutex and signal the main loop via `pane.Interrupt` → `screen.PostEvent(NewEventInterrupt(nil))`.
- `PostEvent` is non-blocking: if the event queue is full the wake-up is dropped. That is safe — the main loop is already processing a queue of pending events, so a render is imminent.
- The initial pane is created by `main` before the loop, or lazily inside `wm.Draw` if the tree is empty.
- Keep the pane `Draw` call inside the manager's `Draw` so PTY `Winsize` resizes run on the main loop.
- Key handling runs on the main loop: `handleKey` returns `false` to forward an event to the focused pane's `HandleEvent`, which writes keystroke bytes to the PTY.
- While a bracketed paste is active (`pasteActive`), every `EventKey` is forwarded verbatim to the focused pane; prefix/command parsing is bypassed so pasted text can never trigger a multiplexer command.