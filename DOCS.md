# DOCS.md — Control flow

`roge` is a terminal multiplexer: each pane runs a real shell in a PTY, and a
small VT emulator decodes the shell's output into a cell grid that is drawn
straight into the tcell screen buffer. There is one OS thread for the UI loop
and one goroutine per pane reading PTY output.

## Control-flow overview

```text
main() [main goroutine]
  |
  +-- 1. open roge.log, log.SetOutput
  +-- 2. tcell.NewScreen + screen.Init, screen.EnablePaste()
  +-- 3. install pane.Interrupt = screen.PostEvent(EventInterrupt)
  +-- 4. layout.NewWindowManager
  +-- 5. wm.NewPane(): pty.Start(shell: $SHELL -> bash -> zsh -> sh)
  +-- 6. spawn per-pane goroutines: readPtyOutput, cmd.Wait
  +-- 7. render(): screen.Clear -> wm.Draw -> screen.Show
  |
  v
PollEvent()                     [blocks; woken by pane.Interrupt()]
  |
  v
CloseDeadPanes(): remove every pane whose shell exited, refocus a neighbor
  |
  +-- panes remain? -- no (shouldQuit = true) --> close every pane -> exit
  |
  v
switch (ev.Type)
  |
  +-- EventKey [pasteActive]   -> forward verbatim to focused pane (no commands)
  |      render()
  |
  +-- EventKey                 -> handleKey()
  |      |-- consumed          -> mutate layout tree
  |      |                        (split/focus/zoom/swap/close/resize/jump)
  |      +-- forwarded         -> FocusedPane.HandleEvent
  |                               -> KeyToBytes -> write PTY
  |      render()
  |
  +-- EventPaste               -> set/clear pasteActive; send ESC[200~ / 201~
  |                               to the focused pane if it enabled mode 2004
  |      render()
  |
  +-- EventResize / EventInterrupt -> render()
  |
  +-- EventError                    -> log only, no render
  |
  v
(loop back to PollEvent)

background goroutines (one set per pane):
  readPtyOutput: read -> lock -> process -> unlock -> pane.Interrupt()
  cmd.Wait:      shell exited -> mark pane dead -> pane.Interrupt()
```

## 1. Startup (`main.go`)

1. Open `roge.log`, redirect the process logger to it.
2. `tcell.NewScreen()` + `screen.Init()` — requires a real TTY; fails if stdin
   is not a TTY.
3. `screen.EnablePaste()` so bracketed-paste sequences become `EventPaste`.
4. Install `pane.Interrupt` to post an `EventInterrupt` to the screen queue
   (background goroutines use it to wake the UI loop).
5. `layout.NewWindowManager()`, then `wm.NewPane()` to create pane 1
   (`pane.NewPane("1")`): spawns a shell (`bash`/`$SHELL`/`zsh`/`sh`, first
   found) under `pty.Start(...)`, creates the 80×24 emulator, starts
   `readPtyOutput` and a `cmd.Wait` goroutine.
6. First `render()`: `screen.Clear` → `wm.Draw(screen, w, h, prefixActive, resizeMode, errorMsg)` → `screen.Show()`.

## 2. Main event loop

`for !shouldQuit { screen.PollEvent() }`, type-switching on the event:

- **`*tcell.EventKey`:** clears `errorMsg`. While a bracketed paste is active
  (`pasteActive`), the key is forwarded **verbatim** to the focused pane so
  pasted text can never be mistaken for a multiplexer command. Otherwise
  `handleKey(ev)` is called; if it returns false (unconsumed), the event is
  handed to `wm.FocusedPane().HandleEvent(ev)`, which converts it to bytes and
  writes them to the PTY. Then `render()`.
- **`*tcell.EventPaste`:** sets/clears `pasteActive`; if the focused shell
  enabled bracketed paste (DECSET 2004), the paste is wrapped with
  `ESC[200~` / `ESC[201~`. Then `render()`.
- **`*tcell.EventResize` / `*tcell.EventInterrupt`:** `render()` only.
- **`*tcell.EventError`:** logged; no render.

`render()` always runs on the main goroutine — this is the only place the
tcell screen buffer is mutated (see §4).

### Key handling (`handleKey`)

Returns `true` when the multiplexer consumes the event, `false` to forward.

1. **Esc:** cancels prefix/resize mode; forwarded to the shell otherwise.
2. **Ctrl+A:** toggles prefix mode (resize mode cancels first).
3. **Resize mode:** one `h/j/k/l` adjusts the focused pane's size
   (`wm.DecreaseSize`/`IncreaseSize`), drawing ends the mode.
4. **Prefix mode:** `1-9` jumps by pane index; `c`/`-` h-split, `C`/`|`
   v-split, `x` closes (last pane → `shouldQuit = true`), `h/j/k/l` focus,
   `z` zoom toggle, `o` rotate, `{`/`}` swap, `r` enters resize mode,
   `q` quits; arrows move focus. Letter commands only match `KeyRune` events
   (unmodified characters), so Ctrl+letter and function keys simply end the
   prefix mode — matching how termbox reported `Ch == 0` for them.
5. **Default:** return false → key forwarded to the focused shell.

## 3. PTY output → screen

`readPtyOutput` (one goroutine per pane, `pane/readPtyOutput`):

```
read(pty) → lock pane mutex → term.process(bytes) → unlock → pane.Interrupt()
```

- `pane.Interrupt` is installed by `main` to `screen.PostEvent(NewEventInterrupt(nil))`;
  posting is safe from any goroutine and wakes a blocked `PollEvent`,
  producing an `EventInterrupt` → `render()`. `PostEvent` is non-blocking — if
  the queue is full the wake-up is dropped, which is safe because the main loop
  is already consuming events.
- The emulator (`pane/vt.go`) parses ESC/CSI/OSC sequences and maintains an
  authoritative grid plus alternate-screen buffer, scrollback, and cursor.
  Raw shell bytes never reach the screen directly.

## 4. Drawing (`wm.Draw`)

1. Lazily create the first pane if the tree is empty.
2. Carve `height - StatusBarHeight` rows for panes; render status bar last.
3. If zoomed, draw only the zoomed pane; else `distribute` recurses over the
   `LayoutNode` tree assigning rectangles
   (`SplitHorizontal` = stacked rows, `SplitVertical` = side-by-side cols).
4. Each `Pane.Draw(x0,y0,x1,y1,isFocused, screen)`:
   - border (four corners, top/bottom, left/right; bright cyan + bold if
     focused, dim gray otherwise);
   - if inner `w`/`h` changed: resize emulator and call
     `pty.Setsize(...)` so the shell sees the new size;
   - copy the emulator grid via `screen.SetContent` and `cellToStyle` — cells
     carry a `vtColor` default / palette index / truecolor RGB, and wide
     (CJK/emoji) runes are drawn at their lead cell while the continuation
     cell is skipped;
   - return the logical cursor (only meaningful when focused; honors DECTCEM
     cursor visibility).
5. Rendered pane cursor drives `screen.ShowCursor`/`HideCursor`; the status bar
   is drawn into the last row; `screen.Show()`.

## 5. Shutdown

- Each loop iteration calls `wm.CloseDeadPanes()`, which removes any pane whose
  shell exited (e.g. `exit`), re-focusing a neighbor. If the last pane closed
  this way, `shouldQuit` is set and the loop exits.
- Closing the last pane with `x` in prefix mode also sets `shouldQuit`.
- Every pane is closed: `cmd.Process.Signal(os.Interrupt)`, wait up to
  150 ms on `done`, kill if still alive, then close the PTY (a pane whose
  process already exited only has its PTY closed).
- `Cmd.Wait` goroutine marks the pane dead (`setDead` calls `pane.Interrupt` so
  the main loop can auto-close it) and closes `done`; `readPtyOutput` unblocks
  on the PTY error, waits on `done`, and exits.

## 6. Concurrency model

- **Screen buffer:** touched only on the main goroutine (`render`/handlers).
  Background code must never call `SetContent`/`Show`.
- **Shared state:** each `Pane` has `mu` guarding the emulator (written by
  `readPtyOutput`, read by `Draw`) and `ptyMtx` guarding PTY writes.
- **Synchronization:** background → UI is one-way via `pane.Interrupt`
  → `screen.PostEvent(EventInterrupt)`.
