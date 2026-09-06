# AGENTS.md

TUI "roge" terminal multiplexer built with `rivo/tview`, `gdamore/tcell/v2`, and `creack/pty`. Go module `github.com/atharva-0123-cs/tview-demo`. It writes a debug log to `roge.log` in the working directory.

## Build & run

- Build: `go build -o tview-demo .` (matches `go.mod` module).
- Run: `./tview-demo` — a real terminal is required; it exits (code 1) if stdin is not a TTY. Do not test with `</dev/null`.
- The binary `tview-demo` is gitignored; always rebuild after editing source before testing (`go build ./...` only type-checks, it does not update the runnable binary).
- No tests, no CI, no linter/formatter config, no README. `go vet ./...` is the only sanity check.

## Architecture

- `main.go` — entrypoint. Creates `Application`, `WindowManager` (layout), and a status bar inside a `Flex`. Owns global input capture (Ctrl+A prefix → c/x/h/l/q).
- `pane` — a `Pane` embeds `*tview.TextView` and wraps a `creack/pty` master/slave running `bash`. Spawns a goroutine (`readPtyOutput`) per pane.
- `layout` — `WindowManager` manages the slice of panes and rebuilds the `Flex` in `updateLayout`.

## Concurrency gotcha (critical)

`tview.Application.Draw()` and `QueueUpdateDraw()` block on an internal queue channel and may only be called **from the app event loop / after `app.Run()` has started**. Calling them during construction (before `Run()`), e.g. `wm.NewPane()` called from `main.main()` before `app.Run()`, triggers `fatal error: all goroutines are asleep - deadlock!`.

Follow this pattern:
- Initial layout/focus setup done before `app.Run()` must **not** call `app.Draw()`.
- Redraws from background goroutines (like `readPtyOutput`) must use `app.QueueUpdateDraw(func(){...})`.
- `WindowManager.NewPane()` is invoked both before `Run()` (initial pane) and at runtime (prefix `c`); it must stay safe for both paths.
