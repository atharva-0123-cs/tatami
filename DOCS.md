`tatami` is a agentic terminal multiplexer: each pane runs a shell in a PTY, and a
`charmbracelet/x/vt` emulator (wrapped by `term/`) decodes the shell's output into
a cell grid that is drawn straight into the tcell screen buffer. There is one OS
thread for the UI loop and one goroutine per pane reading PTY output.

On top of the terminal, an embedded FalkorDB (a Redis graph database) client
turns the multiplexer into shared, queryable memory: a code knowledge graph, a
workspace/process graph, a multi-agent blackboard, and a semantic command
history. The graph is what lets panes and the agents running in them coordinate
across otherwise-isolated PTYs. See §7.

## Control-flow overview

```text
main() [main goroutine]
  |
  +-- 1. open tatami.log, install slog handler (setupLogger: text | json)
  +-- 2. config.Load(), re-install logger with the profile
  +-- 3. falkor.Initialize(cfg) + Startup health check (never fatal)
  +-- 4. app.New(cfg): tcell.NewScreen + screen.Init, EnableMouse (mouse=on)
  +-- 5. install pane.Interrupt = screen.PostEvent(EventInterrupt)
  +-- 6. layout.NewWindowManager, wm.NewPane(): pty.Start(shell: $SHELL -> bash -> zsh -> sh)
  +-- 7. spawn per-pane goroutines: readPtyOutput, cmd.Wait
  +-- 8. defer a.Cleanup(), then a.Run(): render -> PollEvent loop
  |
  +-- Cleanup: close every pane -> screen.Fini -> xterm reset sequences
  |            (also stops the graph monitor + clock goroutines)
  |
  v
a.Run(): PollEvent()                 [blocks; woken by pane.Interrupt()]
  |
  +-- startGraphMonitor() (10s FalkorDB ping), startClock() (1 min tick)
  |
  v
CloseDeadPanes(): remove every pane whose shell exited, refocus a neighbor
  |
  +-- panes remain? -- no (shouldQuit = true) --> exit loop -> Cleanup
  |
  v
switch (ev.Type)
  |
  +-- EventKey [pasteActive]   -> forward verbatim to focused pane (no commands)
  |      Render()
  |
  +-- EventKey                 -> handleKey()
  |      |-- consumed          -> mutate layout tree
  |      |                        (split/focus/zoom/swap/close/resize/jump)
  |      |                        or run a FalkorDB command (g / m / : / s / R / t)
  |      +-- forwarded         -> FocusedPane.HandleEvent
  |                               -> KeyToBytes -> write PTY
  |      Render()
  |
  +-- EventMouse               -> click focuses the pane under the cursor; the
  |                               wheel scrolls the hovered pane (arms copy mode)
  |      Render()
  |
  +-- EventPaste               -> set/clear pasteActive; send ESC[200~ / 201~
  |                               to the focused pane if it enabled mode 2004
  |      Render()
  |
  +-- EventResize / EventInterrupt -> Render()
  |
  +-- EventError                    -> log only, no render
  |
  v
(loop back to PollEvent)

background goroutines (one set per pane, plus two app-wide):
  readPtyOutput:    read -> lock -> term.Process -> unlock -> pane.Interrupt()
  cmd.Wait:         shell exited -> mark pane dead -> pane.Interrupt()
  startGraphMonitor: every 10s ping FalkorDB; on change -> pane.Interrupt()
  startClock:       every minute -> pane.Interrupt() (keeps the status clock)
```

## 1. Startup (`main.go` + `internal/app.New`)

1. Open `tatami.log`, install the global slog handler via `setupLogger`
   (two-phase: defaults first so config warnings are captured, then the loaded
   profile; `text` renders `[LEVEL] time file:line : message`, `json` is the
   structured alternative).
2. `config.Load()` reads `tatami.conf` and `TATAMI_*` overrides; the logger is
   re-installed with the effective profile.
3. `falkor.Initialize(cfg)` builds the globals graph client (no I/O) and
   `GlobalClient.Startup(...)` probes FalkorDB once with a few retries. A
   missing database only logs a warning — graph features reconnect on demand
   (§7).
4. `app.New(cfg)` takes over: `tcell.NewScreen()` + `screen.Init()` — requires
   a real TTY; fails if stdin is not a TTY.
5. `screen.EnableMouse(...)` when `mouse = on`.
6. Install `pane.Interrupt` to post an `EventInterrupt` to the screen queue
   (background goroutines use it to wake the UI loop).
7. `layout.NewWindowManager()`, then `wm.NewPane()` to create pane 1
   (`pane.NewPane("1")`): spawns a shell (`bash`/`$SHELL`/`zsh`/`sh`, first
   found) under `pty.Start(...)`, creates the emulator, starts
   `readPtyOutput` and a `cmd.Wait` goroutine.
8. `main` defers `a.Cleanup()`, then `a.Run()` drives the loop from here;
   `Run` also starts the FalkorDB health monitor and the clock ticker.

All per-loop UI state (prefix/resize/paste/copy modes, scroll offset, error
message, graph prompt/result, quit flag) lives on the `App` struct; there are
no package globals except the `falkor.GlobalClient` handle. The tcell screen may
only be touched on the main goroutine.

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
- **`*tcell.EventMouse`:** primary-button click focuses the pane under the
  cursor (`wm.FocusAt`); a wheel up/down scrolls the hovered pane's scrollback
  by `wheelStep` lines and arms copy mode (scrolling back to the live screen
  disarms it). Only delivered when `mouse = on`. Then `render()`.
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
4. **Prefix mode:** `1-9` switches to window N (`wm.SwitchWindow`); `c`/`-`
   h-split, `C`/`|` v-split, `w` new window, `n` next window, `p`/`W`
   previous window, `x` closes (last window → `shouldQuit = true`),
   `z` zoom toggle, `o` rotate, `{`/`}` swap, `r` enters
   resize mode, `[` enters copy mode, `q` quits; arrows move focus. `:` (or
   `;`) opens the command prompt (`internal/app/command.go`); the FalkorDB
   commands also live here: `g` index the CWD, `m` opens the Cypher prompt
   directly, `s`/`R` save/restore a session, `t` connectivity test, and the
   `cypher` command also opens the Cypher prompt (§7.3).
   Letter commands only match `KeyRune` events (unmodified characters), so
   Ctrl+letter and function keys simply end the prefix mode.
5. **Default:** return false → key forwarded to the focused shell.

Copy mode (`[`) scrolls the focused pane's scrollback (`j`/`k` or `↑`/`↓`,
`PgUp`/`PgDn`, `Home`/`End`, `g`/`G`), copies the visible text (`y`) or exits
(`q`/Esc or any unhandled key). While a graph overlay is open
(`graphInput`/`graphResultMode`) it is modal and consumes every key first.

The status bar is a single row drawn by `wm.renderStatus`: a workspace icon
badge, `[index:name]` tabs for every window (active one highlighted; the badge
turns amber in prefix/resize/copy mode and red on error), and the graph health
`graph:on`/`graph:off` plus a `HH:MM` clock on the right.

## 3. PTY output → screen

`readPtyOutput` (one goroutine per pane, `pane/output.go`):

```
read(pty) → lock pane mutex → term.Process(bytes) → unlock → pane.Interrupt()
```

- `pane.Interrupt` is installed by `main` to `screen.PostEvent(NewEventInterrupt(nil))`;
  posting is safe from any goroutine and wakes a blocked `PollEvent`,
  producing an `EventInterrupt` → `render()`. `PostEvent` is non-blocking — if
  the queue is full the wake-up is dropped, which is safe because the main loop
  is already consuming events.
- The emulator (`term/terminal.go` — a thin adapter over `charmbracelet/x/vt`,
  which does the ESC/CSI/OSC parsing) maintains the authoritative grid plus
  alternate-screen buffer, scrollback, and cursor, and converts the library's
  cells into `term.Cell`/`term.Color`. Raw shell bytes never reach the screen
  directly.

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
     carry a `term.Color` default / palette index / truecolor RGB, and wide
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
- **FalkorDB:** `falkor.Client` guards its connectivity fields with its own
  mutex; health probes run on the UI goroutine while the background monitor
  updates the same fields. The monitor touches neither the screen nor the graph
  directly, only `pane.Interrupt`.
- **Synchronization:** background → UI is one-way via `pane.Interrupt`
  → `screen.PostEvent(EventInterrupt)`.

## 7. FalkorDB integration (`falkor/`)

FalkorDB is a graph database implemented as a Redis module. Tatami speaks to it
through the `go-redis` client using `GRAPH.QUERY` (Cypher over RESP) and plain
Redis pub/sub for low-latency coordination nudges. The integration exists so the
panes — and the AI agents running inside them — share durable, queryable
memory instead of being isolated behind their PTYs.

### 7.1 Client, configuration and connectivity (`falkor/client.go`)

- `falkor.Initialize(cfg)` builds a `Client` (a `redis.Client` plus the graph
  name) and stores it in the package-level `GlobalClient`. It performs no I/O.
- Config keys `falkordb_host` / `falkordb_port` / `falkordb_graph` /
  `falkordb_password` (or the `TATAMI_FALKORDB_HOST` / `_PORT` / `_GRAPH` /
  `_PASSWORD` env overrides) select the endpoint, graph and credentials. The
  password is passed to `redis.Options.Password`; the startup log records only
  whether auth is set (`auth=true/false`), never the secret.
- Connectivity is tracked explicitly, guarded by a mutex, and self-healing:
  - `Startup` — boot probe: a few `PING`s with backoff; never fatal.
  - `Connect` — pings on demand and caches success.
  - `HealthCheck` — always pings, so it detects a server that went away, and
    logs `falkordb connected` / `falkordb connection lost` on a state change.
  - `Query` — runs `GRAPH.QUERY`; a transport error (`isConnError`) marks the
    client disconnected so the next query reconnects.
  - `Connected` / `Status` / `Addr` / `GraphName` expose state for the UI;
    `Close` marks it disconnected and closes the Redis connection.
- `PublishEvent` / `SubscribeEvents` wrap Redis pub/sub — the low-latency
  "nudge" channel agents use to wake each other. The graph stays the durable
  shared memory; pub/sub is only the doorbell.

A missing FalkorDB is never fatal: the multiplexer runs normally and the status
bar shows `graph:off`. `internal/app/graph.go` owns the TUI side of this: the
`startGraphMonitor` goroutine re-probes every 10 s and, on a connectivity
change, calls `pane.Interrupt()` so the main loop re-renders the indicator.

### 7.2 The graph is four related domains

Everything shares one graph (default name `tatami`). Node IDs are
scheme-prefixed and derived from stable keys (project-relative paths, pane
paths, agent ids) so re-indexing is idempotent.

**Code knowledge graph** (`code_schema.go`, `graph.go`, `parser.go`,
`go_parser.go`, `python_parser.go`, `js_parser.go`, `ingest.go`)

```
(:Project)-[:CONTAINS]->(:Package|:File)
(:File)-[:BELONGS_TO]->(:Package)
(:File)-[:IMPORTS]->(:Import)
(:Struct)-[:HAS_FIELD]->(:Field)
(:Function)-[:CALLS]->(:Function)
(:Package)-[:DEPENDS_ON]->(:Package)
```

The parsers turn a source file into graph fragments: Go uses `go/ast`, while
Python and JS/TS use regex/indentation scanners (`parser.go` maps extension →
parser). `ScanProject` walks the tree (skipping hidden/`vendor`/`node_modules`/
`build`/…), merges the fragments, and resolves call sites to `CALLS` edges
(`Resolve`: same-file target first, then any matching simple name; stdlib and
third-party names are dropped). `Client.Ingest` clears the graph and replays
deterministic, idempotent `MERGE` statements — node `MERGE`s before
relationship `MATCH`es, so replay is always safe.

**Workspace / process graph** (`schema.go`, `workspace_graph.go`, `mapper.go`)

```
(:Session)-…->(:Window)-[:ROOT]->(:Node)-[:CHILD]->(:Pane)
(:Pane)-[:WORKING_IN]->(:Directory)
(:Pane)-[:EXECUTES]->(:Process)-[:CHILD_PROCESS]->(:Process)
```

`WorkspaceGraph.AddSession` models a serialized `layout.SessionSpec`: every
window, its split tree, each pane's working directory (shared by path) and the
active window. Process ancestry (`parent_pid → child_pid`) is modelled too.
`SaveSession` stores the full JSON snapshot on a `Session` node (for exact
restore) *and* writes the queryable graph; `RestoreSession` reads it back.

**Multi-agent shared memory** (`agent_graph.go`)

```
(:Agent)-[:FOUND]->(:Finding)-[:REPORTS]->(:Bug)-[:AFFECTS]->(:File)
(:Agent)-[:ASSIGNED_TO]->(:Task)-[:DEPENDS_ON|:BLOCKS]->(:Task)
```

This is the blackboard: agents in different panes write findings, bug reports
and task dependencies and read each other's work with Cypher, with no central
coordinator. `WaitForFinding` is the polling primitive an agent uses to block
until another pane's result exists.

**Semantic command history** (`history_graph.go`)

```
(:Command)-[:AFFECTED]->(:File)
(:Command)-[:EXECUTED]->(:Process)
(:Command)-[:PRODUCES]->(:Output)   // only on failure
```

Each executed command is recorded with its exit code, `status` (`ok`/`failed`),
output summary, cwd and timestamp. This makes "what did I run that fixed this
file?" a graph query: a file reached by a failed command and an ok command is a
troubleshooting path.

### 7.3 How the TUI exposes it

- `Ctrl+A g` — `IndexProject(".")`: scan the CWD and ingest the code graph.
- `Ctrl+A m` — open the Cypher prompt directly (below).
- `Ctrl+A :` (or `;`) — command mode (`internal/app/command.go`): modal prompt
  with history, aliases, `:help` and a completion dropdown (command names and
  aliases, plus pane ids for pane-taking commands). Each command logs its
  outcome. The `cypher [query]` command runs a one-off query, or opens the
  Cypher prompt when given no argument.
- Cypher prompt (`internal/app/graph.go`), reached with `Ctrl+A m` or the
  `cypher` command — rendered as a
  bordered bottom panel with an input line and a completion dropdown. `Tab`
  (and `Shift-Tab`/Backtab, backwards) cycles completions: `:name` builtins in
  the first token, plus Cypher keywords, node labels and relationship types
  (`falkor.SchemaTokens`) anywhere. Enter runs the statement (raw
  `MATCH … RETURN …` or a `:<name>` shortcut); the result opens in a scrollable
  overlay (`j`/`k` scroll, `q`/Esc close).
- `Ctrl+A s` / `Ctrl+A R` — save / restore a named workspace session.
- `Ctrl+A t` — connectivity test query.
- CLI `tatami save [name]` / `tatami restore [name]` operate on sessions
  without starting the UI; the running instance mirrors its workspace to
  `live.json` (see `internal/app/session.go`) so a separate `save` can snapshot
  it.

Builtin shortcuts (`queries.go`), grouped by domain — code: `:files`,
`:packages`, `:imports`, `:structs`, `:functions`, `:callers`, `:calls`,
`:chain`, `:deps <path>`; agents: `:agents`, `:findings`, `:bugs`, `:tasks`;
history: `:history`, `:failures`, `:fixes`; workspace: `:sessions`, `:panes`,
`:procs`.

### 7.4 Why the graph matters

- **Relationships are the product.** Call chains, dependency reachability,
  variable-depth call paths (`CALLS*1..4`), "which files does this bug affect?",
  "which command fixed this file?", and multi-agent task dependencies are all
  recursive traversals. In a graph they are one `MATCH`; in tables or in-memory
  state they need hand-written joins or recursion.
- **Shared, durable memory.** Panes are otherwise isolated PTYs. FalkorDB is the
  common substrate the panes/agents write to and read from, and sessions survive
  a restart as resurrectable multi-pane dev environments.
- **Real work, non-fatal dependency.** When FalkorDB is reachable the graph
  drives indexing, query, coordination, history and session restore; when it is
  not, the multiplexer still runs and only the graph features go dark
  (`graph:off`).
