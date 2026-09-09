# tatami

A terminal multiplexer written in Go. Each pane runs a real shell in a PTY
(`creack/pty`), and a small VT100/xterm-compatible emulator decodes the shell's
output into a cell grid that is drawn straight into the tcell screen buffer.

> **Learning project.** Written from scratch (no multiplexer framework) to
> learn terminals: PTYs, VT/ANSI emulation, tcell event loops, and tiling
> layout trees. Expect rough edges, deliberate minimalism, and TODOs rather
> than production polish.

## Features

- Horizontal (`Ctrl+A c`) and vertical (`Ctrl+A C`) splits in a nested layout tree
- Multiple windows (workspaces), each with its own layout, shown in the status bar
- Vim-style focus movement, zoom, rotate, swap, and proportional resize
- Configurable gap between panes (`pane_gap =` in `tatami.conf`, 0-8 cells)
- Mouse click-to-focus when `mouse = on` (click a pane to make it active)
- Copy mode (`Ctrl+A [`): scroll the pane's scrollback with `j`/`k`, copy with `y`
- Window titles: the shell's OSC 0/2 title is shown on the pane's top border
- Per-pane VT emulator: fullscreen apps (vim, htop, less), alternate screen,
  scroll regions, DEC special graphics, 256-color + truecolor passthrough,
  double-width (CJK) characters
- SGR text attributes: bold, dim, italic, underline, reverse, strikethrough
- Panes close automatically when their shell exits (last pane quits the app)
- Scrollback + copy-to-clipboard (wl-copy/xclip/xsel/pbcopy, temp-file fallback)
- Dark-theme UI: dim-gray pane borders, dark status bar

## Build & run

```sh
go build -o tatami .
./tatami
```

Requires a real TTY (exits 1 if stdin is not a TTY) and a shell in `$PATH`
(`$SHELL` is preferred, falling back to `bash`/`zsh`/`sh`). Debug output is
written to `tatami.log`.

## Keybindings

Prefix: `Ctrl+A`, then:

| Key            | Action                          |
|----------------|---------------------------------|
| `c` / `-`      | horizontal split                |
| `C` / `|`      | vertical split                  |
| `n`            | new window                      |
| `w`            | next window                     |
| `W` / `p`      | previous window                 |
| `h` `j` `k` `l`| move focus                      |
| `x`            | close pane                      |
| `z`            | zoom toggle                     |
| `o`            | rotate siblings                 |
| `{` / `}`      | swap with previous/next sibling |
| `r`            | resize mode (`h` `j` `k` `l`)   |
| `1`-`9`        | jump to pane N                  |
| `[`            | copy mode (`j`/`k` scroll, `y` copy, `q`/Esc exit) |
| `q`            | quit                            |

`Esc` cancels prefix/resize/copy mode; any other key is forwarded to the shell.
When `mouse = on`, clicking a pane focuses it.

The status bar lists all windows on the right; the active window is shown
bracketed, e.g. `[1] 2 3`.

## Layout

```
main.go     entrypoint, tcell init, event loop
keys.go     prefix/resize/copy-mode command parser, key forwarding
state.go    per-loop UI state (prefix/resize/paste/copy modes)
config/     tatami.conf loader + env overrides
format/     human-readable slog handler ("text" log format)
pane/       Pane, PTY + emulator, input/draw/clipboard helpers
layout/     WindowManager: tree, splits, focus, drawing, resize
```

See `DOCS.md` for the detailed control flow and concurrency model (the tcell
screen buffer may only be touched from the main goroutine).

## Testing

```sh
go test ./...
```

Unit tests cover the VT emulator (cursor, erase, scroll regions, alt screen,
ACS, wide chars, SGR), key-to-bytes translation, and layout tree operations.