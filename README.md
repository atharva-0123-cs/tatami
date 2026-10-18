# tatami

An agentic terminal multiplexer written in Go. Each pane runs a real shell in a
PTY (`creack/pty`), and a `charmbracelet/x/vt` emulator decodes the shell's
output into a cell grid that is drawn straight into the tcell screen buffer. On
top of the terminal, an embedded FalkorDB graph and a built-in coding-agent
harness turn the panes into shared, queryable memory that agents can coordinate
through.

## Features

- Horizontal (`Ctrl+A c`) and vertical (`Ctrl+A C`) splits in a nested layout tree
- Multiple windows (workspaces), each with its own layout, shown in the status bar
- Configurable gap between panes (`pane_gap =` in `tatami.conf`, 0-8 cells)
- Mouse when `mouse = on`: click a pane to focus it, scroll the wheel through its scrollback
- Copy mode (`Ctrl+A [`): scroll scrollback with `j`/`k` or `↑`/`↓`,
  page with `PgUp`/`PgDn`, jump with `g`/`G`, copy with `y`
- Window titles: the shell's OSC 0/2 title is shown on the pane's top border
- Per-pane VT emulator: fullscreen apps (vim, htop, less), alternate screen,
  scroll regions, DEC special graphics, 256-color + truecolor passthrough,
  double-width (CJK) characters
- SGR text attributes: bold, dim, italic, underline, reverse, strikethrough
- Panes close automatically when their shell exits (last pane quits the app)
- Scrollback + copy-to-clipboard (wl-copy/xclip/xsel/pbcopy, temp-file fallback)
- Pane selection UI (`Ctrl+A P`) and content search (`Ctrl+A f`)
- Synchronized input across a window's panes (`Ctrl+A S`)
- Command mode (`Ctrl+A ;`): parser, history, tab completion, aliases, help
- Workspace sessions: live-state autosave plus named `save`/`restore`
- Git integration: repository/branch/dirty/ahead-behind/worktree in the status
  bar and `Ctrl+A G`/`d` overlays
- Built-in, model-agnostic coding-agent harness (`agent/`) with environment and
  project detection, a policy-gated command runner, a context protocol, an
  event bus, and an ID-addressed pane controller
- Embedded FalkorDB knowledge graphs: code, workspace/process, multi-agent
  memory, agent events and semantic command history

## Build & run

```sh
git checkout hackton      # the hackathon submission branch (not main)
go build -o tatami .
./tatami
```

Requires a real TTY (exits 1 if stdin is not a TTY) and a shell in `$PATH`
(`$SHELL` is preferred, falling back to `bash`/`zsh`/`sh`). Debug output is
written to `tatami.log`.

Subcommands operate on FalkorDB session snapshots without starting the UI:

```sh
tatami save [name]      # snapshot the running workspace into FalkorDB
tatami restore [name]   # rebuild and run the workspace from a saved session
tatami help             # usage
```

The running instance continuously mirrors its layout to a local live-state file
(`$XDG_STATE_HOME/tatami/live.json`, or `~/.local/state/tatami/live.json`), which
`tatami save` reads. A reachable FalkorDB is required for `save`/`restore`.

## Keybindings

Prefix: `Ctrl+A`, then:

| Key            | Action                          |
|----------------|---------------------------------|
| `c` / `-`      | horizontal split                |
| `C` / `|`      | vertical split                  |
| `w`            | new window                      |
| `n`            | next window                     |
| `p` / `W`      | previous window                 |
| `x`            | close pane                      |
| `z`            | zoom toggle                     |
| `o`            | rotate siblings                 |
| `{` / `}`      | swap with previous/next sibling |
| `r`            | resize mode (`h` `j` `k` `l`)   |
| `1`-`9`        | jump to window N                |
| `[`            | copy mode (`j`/`k` or `↑`/`↓` scroll, `PgUp`/`PgDn` page, `g`/`G` top/bottom, `y` copy, `q`/Esc exit; mouse wheel scrolls) |
| `t`            | FalkorDB connectivity test      |
| `g`            | index the working directory into FalkorDB |
| `m`            | Cypher prompt (query the graph; results in a scrollable overlay) |
| `:` / `;`      | command prompt (command mode, see below) |
| `P`            | pane selection UI (`j`/`k`, Enter select) |
| `f`            | search pane contents            |
| `S`            | toggle synchronized input in the window |
| `s` / `R`      | save / restore a workspace session |
| `G` / `d`      | Git status / Git diff overlay   |
| `q`            | quit                            |

`Esc` cancels prefix/resize/copy mode; any other key is forwarded to the shell.
When `mouse = on`, clicking a pane focuses it and the mouse wheel scrolls the
hovered pane through its scrollback.

The status bar shows a workspace badge, then `[index:name]` tabs for every
window (the active one highlighted), with the FalkorDB health indicator, the Git
summary, a `sync` marker when synchronized input is on, and a clock on the right.

## Code knowledge graph (FalkorDB)

With a FalkorDB instance reachable (see `falkordb_host`/`falkordb_port` in
`tatami.conf`), `Ctrl+A g` scans the working directory (Go, Python, JS/TS),
builds a graph of projects, packages, files, imports, functions, structs,
fields and variables, and ingests it into the configured graph. `Ctrl+A m`
opens a Cypher prompt (also reachable via the `cypher` command in command mode);
type a raw `MATCH ... RETURN ...` query or a shortcut
(`:files`, `:deps <path>`, `:callers <fn>`, `:chain <fn>`, ...), and the
results appear in a scrollable overlay (`j`/`k` scroll, `q`/Esc close). The
prompt has a completion dropdown: `Tab` (and `Shift-Tab` backwards) completes
`:name` builtins, Cypher keywords, node labels and relationship types.

## Command mode

`Ctrl+A :` (or `Ctrl+A ;`) opens a modal prompt with command history
(`Up`/`Down`), tab completion (command names, aliases and, for pane-taking
commands, pane ids), a completion dropdown, aliases and built-in help
(`:help`). Commands run on the main loop; agent commands use a bounded timeout.
Each command is logged on completion (success or failure) via the structured
logger. The Cypher prompt is reached with the `cypher` command.

| Command | Alias | Action |
|---------|-------|--------|
| `help` / `?` | | show command reference |
| `split` / `vsplit` | `sp` / `vs` | create a horizontal / vertical split |
| `new-window [name]` | `nw` | create a window |
| `rename-window <name>` | | rename the active window |
| `next-window` / `prev-window` | `n` / `pw` | switch windows |
| `list-windows` / `list-panes` | `lsw` / `ls` | list windows / panes |
| `select-pane <id\|index>` | `sel` | focus a pane |
| `rename-pane <id> <name>` | | give a pane a name |
| `close-pane <id\|index>` | `kill` | close a pane |
| `send <id> <text>` | | send literal text to a pane |
| `run <id> <command>` | `sh` | run a command in a pane |
| `resize <id> <h\|v> <delta>` | | resize a pane |
| `choose-pane` | | open the pane selection UI |
| `find` | | search pane contents |
| `synchronize-panes` | | toggle synchronized input |
| `env` / `project` | | show detected environment / project |
| `git` | | show Git status |
| `agent-context` | | render the assembled agent context |
| `agent-start <name> [dir]` | `as` | start a named agent |
| `agent-list` | `agents` | list running agents |
| `agent-switch <name>` | `ac` | make an agent active |
| `agent-status [name]` | `astat` | show agent status |
| `agent-run <name> <cmd>` | `ar` | run a command with an agent |
| `agent-stop <name>` | | stop an agent |
| `cypher [query]` | | open the Cypher prompt, or run a query |
| `quit` | `q` | quit tatami |

## Running FalkorDB

The graph features need a FalkorDB server. The quickest way is Docker:

```sh
docker run -p 6379:6379 -p 3000:3000 -it --rm \
  -e REDIS_ARGS="--requirepass pass" \
  falkordb/falkordb:latest
```

Then point tatami at it in `tatami.conf`:

```conf
falkordb_host = localhost
falkordb_port = 6379
falkordb_graph = tatami
falkordb_password = pass
```

`falkordb_password` may also be set via `TATAMI_FALKORDB_PASSWORD` (useful to
keep the secret out of the file). Tatami is never fatal without FalkorDB: it
logs a warning at startup and keeps running, graph features reconnect on demand,
and the status bar shows `graph:on`/`graph:off`.

## Graph data model

Everything the multiplexer and the agents in it know lives in one FalkorDB
graph. Four views share it (labels/relationships in `falkor/schema.go` and
`falkor/code_schema.go`):

**Code knowledge graph** — `Project`-[:CONTAINS]->`Package`-[:CONTAINS]->`File`,
`File`-[:IMPORTS]->`Import`, `File`-[:CONTAINS]->`Function`/`Struct`-[:HAS_FIELD]->`Field`/`Variable`,
`Function`-[:CALLS]->`Function`, `Package`-[:DEPENDS_ON]->`Package`.

**Workspace / process graph** — `Window`-[:ROOT|CHILD]->`Node`, node-[:CONTAINS]->`Pane`,
`Pane`-[:WORKING_IN]->`Directory`, `Process`-[:CHILD_PROCESS]->`Process`,
`Process`-[:EXECUTES]->`Pane`.

**Multi-agent memory** — `Agent`-[:FOUND]->`Finding`-[:REPORTS]->`Bug`,
`Bug`-[:AFFECTS]->`File`, `Agent`-[:ASSIGNED_TO]->`Task`,
`Task`-[:DEPENDS_ON|BLOCKS]->`Task`.

**Agent events / history** — `Command` nodes with `exit_code`, `status`
(`ok`/`failed`) and `summary`, `Command`-[:AFFECTED]->`File`,
`Command`-[:PRODUCES]->`Output`, and `Session` snapshots.

Example queries (the same ones the `:name` shortcuts use):

```cypher
-- call chain (up to 4 hops) from a function
MATCH p=(a:Function)-[:CALLS*1..4]->(b:Function)
RETURN a.name AS from, b.name AS to, length(p) AS depth ORDER BY depth;

-- open bugs and the files they affect
MATCH (b:Bug) WHERE b.status = "open" OPTIONAL MATCH (b)-[:AFFECTS]->(f:File)
RETURN b.id, b.severity, f.path;

-- a failing command and the command that later fixed the same file
MATCH (fail:Command {status:"failed"})-[:AFFECTED]->(f:File)<-[:AFFECTED]-(ok:Command {status:"ok"})
RETURN f.path AS file, fail.command AS failing, ok.command AS fixing;
```

Builtin shortcuts: `:files`, `:packages`, `:imports`, `:structs`, `:functions`,
`:callers`, `:calls`, `:chain`, `:agents`, `:findings`, `:bugs`, `:tasks`,
`:history`, `:failures`, `:fixes`, `:sessions`, `:panes`, `:procs`.

## Agentic coding harness

`agent/` is a model-agnostic coding-agent harness. It detects the host
environment (OS, shell, toolchains) and project (Go/C/C++/Rust/Python/JS/TS/
Java, plus README/AGENTS.md/CLAUDE.md/.cursor/.github/build+test config),
assembles a size-limited context (focused pane, layout, Git, recent commands,
current errors, terminal output), and runs commands through a policy-gated
`Runner` (allow/deny lists, read-only and no-network modes, destructive-command
confirmation, working-directory + environment control, timeout and cancellation)
capturing stdout/stderr, exit codes, compiler errors and test failures.

`agent.Registry` runs several named agents (each with a stable session ID) and
`agent.Controller` gives them a permission-checked, ID-addressed view of panes
and windows (`layout/control.go`): list panes/windows (with metadata), create or
close panes and windows, select, send input, read output and resize. Every
operation re-resolves the pane by its stable ID, and the `ControlPolicy` can
restrict the allowed capabilities and the exact pane/window IDs an agent may
touch, so an agent cannot accidentally drive the wrong pane.

`agent.EventBus` is a non-blocking fan-out bus for internal events:
`pane.created`/`pane.closed`, `command.started`/`finished`/`failed`,
`terminal.output`/`terminal.resize`, `git.changed`, and
`build.failed`/`test.failed`/`test.passed`. The runner publishes command and
build/test events; the pane handlers publish pane events; the Git monitor
publishes `git.changed`.

Permissions (`agent.Policy`) gate execution: allow/deny lists, denied patterns,
read-only mode, a no-network mode, environment-variable allowlists, and
confirmation of destructive commands (with `LooksDestructive` covering `rm`,
`dd`, force-push-style Git and shell redirections). The `Runner` runs each
command in its own process group with a timeout and cancellation, so a runaway
child is killed with the group.

`prefix+:` (or `prefix+;`) exposes the harness through command mode (`env`, `project`, `git`,
`agent-context`, `agent-start`, `agent-run`, `agent-list`, `agent-switch`,
`agent-status`, `agent-stop`, `send`, `run`, `choose-pane`, `find`,
`synchronize-panes`, ...). Type `:help`.

Git integration (`git/`) detects repositories, branch, dirty/ahead/behind,
commit and linked worktrees, shown in the status bar and via `prefix+G`/`d`, and
feeds an agent-aware Git context with safety checks.

## Layout

```
main.go          thin launcher: logger + config, then app.New/Run
internal/app/    App: tcell screen, event loop, key handling, cleanup
config/          tatami.conf loader + env overrides
format/          human-readable slog handler ("text" log format)
term/            screen emulator: adapter over charmbracelet/x/vt (grid, scrollback)
pane/            Pane: PTY + emulator wiring, input/draw/clipboard helpers
window/          workspace model: layout tree (Node), Window save slots, Registry
layout/          WindowManager: tree splits, focus, drawing, resize, status bar
agent/           coding-agent harness: env/project detection, policy, runner,
                 context protocol, events, named-agent registry, pane controller
git/             Git repository detection and status/diff helpers
falkor/          FalkorDB graph client + code/workspace/agent/history graphs
```

See `DOCS.md` for the detailed control flow and concurrency model (the tcell
screen buffer may only be touched from the main goroutine).

## Hackathon submission (Graph Hacks by WeMakeDevs x FalkorDB)

- **Branch.** The submitted project lives on the **`hackton`** branch — pull and
  build that branch (`git checkout hackton`), not `main`.
- **AI assistant disclosure.** This project was built with AI coding assistants
  (an agentic CLI) used for implementation help, refactoring and documentation.
  All graph modeling, architecture, query design and verification were done by
  the author(s), who can explain the graph model, the Cypher queries and the
  technical decisions.
- **Data.** Only public data (the source tree you point the indexer at),
  synthetic fixtures, and the authors' own data are used. No private,
  login-protected, paywalled or personal data is involved.
- **Setup.** See [Build & run](#build--run); FalkorDB can be run via Docker
  (`docker run -p 6379:6379 falkordb/falkordb`) or any other deployment, and the
  graph data model and queries are documented above.
- **Track.** **Agents That Act on Connected Data (Track 1)** and **Agent Memory
  and Coordination (Track 2).** The code knowledge graph drives multi-hop code
  reasoning and agent tools (`:chain`, `:callers`, `:deps`, fix-finder), and the
  agent graph persists memory across sessions and shares state between agents.

## License

MIT — see [LICENSE](LICENSE).

## Testing

```sh
go test ./...
```

Unit tests cover the VT emulator (cursor, erase, scroll regions, alt screen,
ACS, wide chars, SGR), key-to-bytes translation, layout tree operations, the
config loader, the FalkorDB code/agent/history graph builders, the Git helpers,
and the agent harness (environment/project detection, policy, runner, context,
events and the named-agent registry). A single test can be run with, e.g.:

```sh
go test ./agent/ -run TestRegistry
```
