# Tatami TODO

Tatami is a terminal multiplexer with an integrated FalkorDB graph engine.

## 1. FalkorDB Graph Integration (https://docs.falkordb.com/)

### FalkorDB Core & Client Setup
- [x] Implement FalkorDB connection manager using Redis/FalkorDB Go client.
- [x] Add configuration settings in `tatami.conf` for FalkorDB host, port, and credentials (`falkordb_host`, `falkordb_port`).
- [] Add startup health-check and connection error handling.

### Codebase & Dependency Knowledge Graph
- [x] Implement AST parser / repository scanner for Go, Python, and JS/TS projects.
- [x] Ingest code structures (Files, Functions, Structs, Imports) into FalkorDB graph.
- [x] Implement Cypher query parser and execution engine.
- [x] Add a Cypher Query interactive TUI pane / command prompt in Tatami.
- [x] Support graph-based dependency & call-chain traversals (`MATCH ... RETURN ...`).

### Persistent Workspace & Process Graph
- [ ] Model windows, layout trees (splits), active panes, and working directories as graph nodes in FalkorDB.
- [ ] Model process execution ancestry (`parent_pid -> child_pid`) in FalkorDB.
- [ ] Implement session save / snapshot command (`tatami save`).
- [ ] Implement session restore command (`tatami restore`) to instantly resurrect multi-pane dev environments.

### Multi-Agent Shared Memory Graph
- [ ] Build shared memory graph schema in FalkorDB for multi-agent coordination.
- [ ] Allow AI agents in different panes to read/write findings, bug reports, and task dependencies (`(Agent)-[:FOUND]->(Bug)-[:AFFECTS]->(File)`).
- [ ] Add agent synchronization primitives via FalkorDB pub/sub or graph polling.

### Semantic Terminal History & Analytics Graph
- [ ] Store executed shell commands, exit codes, output error summaries, and affected files as a directed graph in FalkorDB.
- [ ] Query command success patterns and troubleshooting paths using Cypher pattern matching.

## 2. Agentic Coding Harness

### Built-in Tatami Agent

- [ ] Design the built-in coding-agent harness.
- [ ] Detect the environment Tatami is running in:
  - [ ] Operating system
  - [ ] Shell
  - [ ] Current working directory
  - [ ] Git repository
  - [ ] Git branch
  - [ ] Git status
  - [ ] Available compilers/interpreters
  - [ ] Available package managers
  - [ ] Available build systems
- [ ] Build a machine-readable environment description.
- [ ] Detect project type automatically:
  - [ ] Go
  - [ ] C/C++
  - [ ] Rust
  - [ ] Python
  - [ ] JavaScript/TypeScript
  - [ ] Java
  - [ ] Other common ecosystems
- [ ] Detect project metadata:
  - [ ] Git repository
  - [ ] README
  - [ ] AGENTS.md
  - [ ] CLAUDE.md
  - [ ] .cursor/
  - [ ] .github/
  - [ ] Build configuration
  - [ ] Test configuration
- [ ] Provide the agent with relevant project context.
- [ ] Provide the agent with terminal/pane context.
- [ ] Allow the agent to execute commands through a controlled interface.
- [ ] Allow the agent to inspect files.
- [ ] Allow the agent to modify files.
- [ ] Capture command output.
- [ ] Capture command exit codes.
- [ ] Capture compiler errors.
- [ ] Capture test failures.
- [ ] Feed failures back to the agent.
- [ ] Add command execution timeout handling.
- [ ] Add process cancellation.
- [ ] Add working-directory control.
- [ ] Add environment-variable control.
- [ ] Add permission/sandbox controls.
- [ ] Add confirmation mode for destructive commands.

### Agent Context

- [ ] Define an agent context protocol.
- [ ] Expose current pane information.
- [ ] Expose current working directory.
- [ ] Expose active window/pane layout.
- [ ] Expose Git state.
- [ ] Expose recent command history.
- [ ] Expose relevant terminal output.
- [ ] Expose current errors.
- [ ] Expose project instructions.
- [ ] Prevent unnecessary terminal history from being sent to the model.
- [ ] Add context-size limits.
- [ ] Add context filtering/summarization.

## 3. Agent ↔ Pane Integration

- [ ] Allow an agent to create a pane.
- [ ] Allow an agent to create a window.
- [ ] Allow an agent to select a pane.
- [ ] Allow an agent to execute commands in a pane.
- [ ] Allow an agent to read pane output.
- [ ] Allow an agent to send input to a pane.
- [ ] Allow an agent to resize panes.
- [ ] Allow an agent to close panes.
- [ ] Allow agents to identify panes by stable IDs.
- [ ] Add pane metadata.
- [ ] Add window metadata.
- [ ] Prevent agents from accidentally controlling the wrong pane.
- [ ] Add explicit agent permissions.

## 4. Agent Events

- [ ] Define internal agent events.
- [ ] Define pane-created event.
- [ ] Define pane-closed event.
- [ ] Define command-started event.
- [ ] Define command-finished event.
- [ ] Define command-failed event.
- [ ] Define terminal-output event.
- [ ] Define terminal-resize event.
- [ ] Define Git-state-changed event.
- [ ] Define build-failed event.
- [ ] Define test-failed event.
- [ ] Define test-passed event.

## 5. Terminal / VT Emulator

- [x] Improve VT parser coverage.
- [x] Add more CSI sequences.
- [x] Add more OSC sequences.
- [x] Verify alternate-screen behavior.
- [x] Verify scroll regions.
- [ ] Verify insert/delete character operations.
- [ ] Verify insert/delete line operations.
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
- [ ] Pane naming.
- [ ] Pane selection UI.
- [ ] Pane search.
- [x] Pane zoom.
- [x] Pane swapping.
- [x] Pane movement.
- [ ] Pane synchronization.
- [ ] Session management.
- [ ] Attach to existing session.
- [ ] Detach from session.
- [ ] Reattach after terminal disconnect.
- [ ] Persistent sessions.
- [ ] Session restore.
- [ ] Save layout.
- [ ] Restore layout.

## 8. Command Mode

- [ ] Add command prompt.
- [ ] Add command parser.
- [ ] Add command history.
- [ ] Add command completion.
- [ ] Add command aliases.
- [ ] Add command help.
- [ ] Add command to create panes.
- [ ] Add command to create windows.
- [ ] Add command to run agents.
- [ ] Add command to switch agents.
- [ ] Add command to inspect agent status.

## 9. Configuration

- [x] Expand tatami.conf.
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

- [ ] Detect Git repositories.
- [ ] Show branch in status bar.
- [ ] Show dirty state.
- [ ] Show ahead/behind state.
- [ ] Show current commit.
- [ ] Detect worktrees.
- [ ] Agent-aware Git context.
- [ ] Add Git status command.
- [ ] Add optional Git diff view.
- [ ] Add agent Git safety checks.

## 11. Observability

- [x] Improve structured logging.
- [ ] Add agent session IDs.
- [ ] Add pane IDs to logs.
- [ ] Add command IDs.
- [ ] Add execution timing.
- [ ] Add agent token/cost tracking where available.
- [x] Add debug mode.
- [x] Add trace mode.
- [x] Add crash diagnostics.

## 12. Testing

- [x] Unit tests for layout.
- [x] Unit tests for VT parser.
- [x] Unit tests for input handling.
- [ ] PTY integration tests.
- [ ] End-to-end terminal tests.
- [ ] Agent harness tests.
- [ ] External agent tests.
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

- [ ] Define agent permission model.
- [ ] Separate trusted and untrusted commands.
- [ ] Protect environment variables.
- [ ] Prevent accidental destructive commands.
- [ ] Add command confirmation policy.
- [ ] Add filesystem access policy.
- [ ] Add network access policy.
- [ ] Add agent sandboxing where possible.
- [ ] Audit shell command execution.
- [ ] Audit external-agent integration.
