# Tatami TODO

Tatami is a terminal multiplexer with an integrated FalkorDB graph engine.

## 1. FalkorDB Graph Integration (https://docs.falkordb.com/)

### FalkorDB Core & Client Setup
- [x] Implement FalkorDB connection manager using Redis/FalkorDB Go client.
- [x] Add configuration settings in `tatami.conf` for FalkorDB host, port, graph and credentials (`falkordb_host`, `falkordb_port`, `falkordb_graph`, `falkordb_password`).
- [x] Add startup health-check and connection error handling.

### Codebase & Dependency Knowledge Graph
- [x] Implement AST parser / repository scanner for Go, Python, and JS/TS projects.
- [x] Ingest code structures (Files, Functions, Structs, Imports) into FalkorDB graph.
- [x] Implement Cypher query parser and execution engine.
- [x] Add a Cypher Query interactive TUI pane / command prompt in Tatami.
- [x] Support graph-based dependency & call-chain traversals (`MATCH ... RETURN ...`).

### Persistent Workspace & Process Graph
- [x] Model windows, layout trees (splits), active panes, and working directories as graph nodes in FalkorDB.
- [x] Model process execution ancestry (`parent_pid -> child_pid`) in FalkorDB.
- [x] Implement session save / snapshot command (`tatami save`).
- [x] Implement session restore command (`tatami restore`) to instantly resurrect multi-pane dev environments.

### Multi-Agent Shared Memory Graph
- [x] Build shared memory graph schema in FalkorDB for multi-agent coordination.
- [x] Allow AI agents in different panes to read/write findings, bug reports, and task dependencies (`(Agent)-[:FOUND]->(Bug)-[:AFFECTS]->(File)`).
- [x] Add agent synchronization primitives via FalkorDB pub/sub or graph polling.

### Semantic Terminal History & Analytics Graph
- [x] Store executed shell commands, exit codes, output error summaries, and affected files as a directed graph in FalkorDB.
- [x] Query command success patterns and troubleshooting paths using Cypher pattern matching.

## 2. Agentic Coding Harness

### Built-in Tatami Agent

- [x] Design the built-in coding-agent harness.
- [x] Detect the environment Tatami is running in:
  - [x] Operating system
  - [x] Shell
  - [x] Current working directory
  - [x] Git repository
  - [x] Git branch
  - [x] Git status
  - [x] Available compilers/interpreters
  - [x] Available package managers
  - [x] Available build systems
- [x] Build a machine-readable environment description.
- [x] Detect project type automatically:
  - [x] Go
  - [x] C/C++
  - [x] Rust
  - [x] Python
  - [x] JavaScript/TypeScript
  - [x] Java
  - [x] Other common ecosystems
- [x] Detect project metadata:
  - [x] Git repository
  - [x] README
  - [x] AGENTS.md
  - [x] CLAUDE.md
  - [x] .cursor/
  - [x] .github/
  - [x] Build configuration
  - [x] Test configuration
- [x] Provide the agent with relevant project context.
- [x] Provide the agent with terminal/pane context.
- [x] Allow the agent to execute commands through a controlled interface.
- [x] Allow the agent to inspect files.
- [x] Allow the agent to modify files.
- [x] Capture command output.
- [x] Capture command exit codes.
- [x] Capture compiler errors.
- [x] Capture test failures.
- [x] Feed failures back to the agent.
- [x] Add command execution timeout handling.
- [x] Add process cancellation.
- [x] Add working-directory control.
- [x] Add environment-variable control.
- [x] Add permission/sandbox controls.
- [x] Add confirmation mode for destructive commands.

### Agent Context

- [x] Define an agent context protocol.
- [x] Expose current pane information.
- [x] Expose current working directory.
- [x] Expose active window/pane layout.
- [x] Expose Git state.
- [x] Expose recent command history.
- [x] Expose relevant terminal output.
- [x] Expose current errors.
- [x] Expose project instructions.
- [x] Prevent unnecessary terminal history from being sent to the model.
- [x] Add context-size limits.
- [x] Add context filtering/summarization.

## 3. Agent ↔ Pane Integration

- [x] Allow an agent to create a pane.
- [x] Allow an agent to create a window.
- [x] Allow an agent to select a pane.
- [x] Allow an agent to execute commands in a pane.
- [x] Allow an agent to read pane output.
- [x] Allow an agent to send input to a pane.
- [x] Allow an agent to resize panes.
- [x] Allow an agent to close panes.
- [x] Allow agents to identify panes by stable IDs.
- [x] Add pane metadata.
- [x] Add window metadata.
- [x] Prevent agents from accidentally controlling the wrong pane.
- [x] Add explicit agent permissions.

## 4. Agent Events

- [x] Define internal agent events.
- [x] Define pane-created event.
- [x] Define pane-closed event.
- [x] Define command-started event.
- [x] Define command-finished event.
- [x] Define command-failed event.
- [x] Define terminal-output event.
- [x] Define terminal-resize event.
- [x] Define Git-state-changed event.
- [x] Define build-failed event.
- [x] Define test-failed event.
- [x] Define test-passed event.

## 5. Terminal / VT Emulator

> The hand-written emulator was replaced by a thin adapter over
> `charmbracelet/x/vt` (`term/terminal.go`), which owns ESC/CSI/OSC parsing,
> the grid, alternate screen, scroll regions and mouse tracking modes. The
> adapter adds copy-mode scrolling, text extraction and the `term.Cell`/
> `term.Color` model the UI draws. Items below marked done are now provided by
> the library; the remaining gaps are in the adapter (e.g. it renders the first
> rune of a grapheme cluster and ignores OSC 8 link metadata).

- [x] Improve VT parser coverage.
- [x] Add more CSI sequences.
- [x] Add more OSC sequences.
- [x] Verify alternate-screen behavior.
- [x] Verify scroll regions.
- [x] Verify insert/delete character operations.
- [x] Verify insert/delete line operations.
- [x] Improve SGR handling.
- [x] Improve truecolor handling.
- [x] Improve 256-color handling.
- [x] Improve Unicode handling.
- [x] Improve CJK width handling.
- [ ] Improve emoji width handling.
- [ ] Handle combining characters.
- [ ] Handle hyperlinks.
- [ ] Handle OSC 52 clipboard.
- [x] Handle terminal title changes.
- [x] Improve scrollback.
- [x] Add VT compatibility tests.
- [x] Test against vim.
- [x] Test against neovim.
- [ ] Test against htop.
- [x] Test against less.
- [ ] Test against tmux.
- [x] Test against shells.

## 6. PTY / Process Management

- [x] Improve shell detection.
- [x] Improve PTY lifecycle handling.
- [ ] Handle SIGHUP correctly.
- [x] Handle SIGWINCH correctly.
- [x] Improve process cleanup.
- [ ] Handle orphaned processes.
- [x] Handle shell crashes.
- [x] Handle PTY read errors.
- [x] Handle PTY write errors.
- [x] Add configurable shell.
- [ ] Add configurable environment.
- [ ] Add configurable startup command.
- [ ] Add configurable login/non-login shell mode.

## 7. Multiplexer Features

- [x] Multiple windows.
- [ ] Window renaming.
- [x] Window list.
- [x] Pane naming.
- [x] Pane selection UI.
- [x] Pane search.
- [x] Pane zoom.
- [x] Pane swapping.
- [x] Pane movement.
- [x] Pane synchronization.
- [x] Copy-mode scrollback (`j`/`k`, arrows, `PgUp`/`PgDn`, `g`/`G`) + clipboard.
- [x] Mouse wheel scrollback navigation.
- [x] Session management.
- [ ] Attach to existing session.
- [ ] Detach from session.
- [ ] Reattach after terminal disconnect.
- [ ] Persistent sessions.
- [x] Session restore.
- [x] Save layout.
- [x] Restore layout.

## 8. Command Mode

- [x] Add command prompt.
- [x] Add command parser.
- [x] Add command history.
- [x] Add command completion.
- [x] Add command aliases.
- [x] Add command help.
- [x] Add command to create panes.
- [x] Add command to create windows.
- [x] Add command to run agents.
- [x] Add command to switch agents.
- [x] Add command to inspect agent status.

## 9. Configuration

- [x] Expand tatami.conf.
- [x] Configure FalkorDB endpoint, graph and credentials (`falkordb_host`/`_port`/`_graph`/`_password`).
- [x] Configure key bindings.
- [x] Configure prefix key.
- [x] Configure colors.
- [x] Configure pane borders.
- [x] Configure status bar.
- [x] Configure shell.
- [ ] Configure agents.
- [ ] Configure agent permissions.
- [x] Configure logging.
- [ ] Configure scrollback.
- [ ] Configure startup layout.
- [x] Add configuration validation.
- [ ] Add configuration reload.

## 10. Git Integration

- [x] Detect Git repositories.
- [x] Show branch in status bar.
- [x] Show dirty state.
- [x] Show ahead/behind state.
- [x] Show current commit.
- [x] Detect worktrees.
- [x] Agent-aware Git context.
- [x] Add Git status command.
- [x] Add optional Git diff view.
- [x] Add agent Git safety checks.

## 11. Observability

- [x] Improve structured logging.
- [x] Add agent session IDs.
- [ ] Add pane IDs to logs.
- [x] Add command IDs.
- [x] Add execution timing.
- [x] Add agent token/cost tracking where available.
- [x] Add debug mode.
- [x] Add trace mode.
- [x] Add crash diagnostics.

## 12. Testing

- [x] Unit tests for layout.
- [x] Unit tests for VT parser.
- [x] Unit tests for input handling.
- [ ] PTY integration tests.
- [ ] End-to-end terminal tests.
- [x] Agent harness tests.
- [x] External agent tests.
- [ ] Resize tests.
- [ ] Unicode tests.
- [ ] Concurrent pane tests.
- [ ] Process cleanup tests.
- [ ] Fuzz VT parser.
- [ ] Run race detector regularly.

## 13. Performance

- [ ] Benchmark VT parsing.
- [ ] Benchmark rendering.
- [ ] Benchmark large scrollback.
- [ ] Reduce unnecessary renders.
- [ ] Coalesce PTY wake-up events.
- [ ] Avoid unnecessary cell copies.
- [ ] Profile allocations.
- [ ] Profile large pane counts.
- [ ] Test 100+ panes.
- [ ] Test high-output programs.

## 14. Documentation

- [x] Architecture documentation.
- [ ] Agent architecture documentation.
- [ ] Agent protocol documentation.
- [x] Configuration reference.
- [x] Key binding reference.
- [ ] Plugin/agent integration guide.
- [x] Developer guide.
- [ ] Security model.
- [x] Concurrency model.
- [x] VT emulator documentation.
- [x] Contribution guide.

## 15. Security

- [x] Define agent permission model.
- [x] Separate trusted and untrusted commands.
- [x] Protect environment variables.
- [x] Prevent accidental destructive commands.
- [x] Add command confirmation policy.
- [x] Add filesystem access policy.
- [x] Add network access policy.
- [x] Add agent sandboxing where possible.
- [x] Audit shell command execution.
- [ ] Audit external-agent integration.
