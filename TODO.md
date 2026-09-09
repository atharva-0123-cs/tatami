# Tatami TODO

Tatami is an agentic terminal multiplexer. The core terminal-multiplexing
foundation is already implemented; this TODO tracks the next stages toward
agent-aware development workflows.

## 1. Agentic Coding Harness

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

- [ ] Improve VT parser coverage.
- [ ] Add more CSI sequences.
- [ ] Add more OSC sequences.
- [ ] Verify alternate-screen behavior.
- [ ] Verify scroll regions.
- [ ] Verify insert/delete character operations.
- [ ] Verify insert/delete line operations.
- [ ] Improve SGR handling.
- [ ] Improve truecolor handling.
- [ ] Improve 256-color handling.
- [ ] Improve Unicode handling.
- [ ] Improve CJK width handling.
- [ ] Improve emoji width handling.
- [ ] Handle combining characters.
- [ ] Handle hyperlinks.
- [ ] Handle OSC 52 clipboard.
- [ ] Handle terminal title changes.
- [ ] Improve scrollback.
- [ ] Add VT compatibility tests.
- [ ] Test against vim.
- [ ] Test against neovim.
- [ ] Test against htop.
- [ ] Test against less.
- [ ] Test against tmux.
- [ ] Test against shells.

## 6. PTY / Process Management

- [ ] Improve shell detection.
- [ ] Improve PTY lifecycle handling.
- [ ] Handle SIGHUP correctly.
- [ ] Handle SIGWINCH correctly.
- [ ] Improve process cleanup.
- [ ] Handle orphaned processes.
- [ ] Handle shell crashes.
- [ ] Handle PTY read errors.
- [ ] Handle PTY write errors.
- [ ] Add configurable shell.
- [ ] Add configurable environment.
- [ ] Add configurable startup command.
- [ ] Add configurable login/non-login shell mode.

## 7. Multiplexer Features

- [ ] Multiple windows.
- [ ] Window renaming.
- [ ] Window list.
- [ ] Pane naming.
- [ ] Pane selection UI.
- [ ] Pane search.
- [ ] Pane zoom.
- [ ] Pane swapping.
- [ ] Pane movement.
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

- [ ] Expand tatami.conf.
- [ ] Configure key bindings.
- [ ] Configure prefix key.
- [ ] Configure colors.
- [ ] Configure pane borders.
- [ ] Configure status bar.
- [ ] Configure shell.
- [ ] Configure agents.
- [ ] Configure agent permissions.
- [ ] Configure logging.
- [ ] Configure scrollback.
- [ ] Configure startup layout.
- [ ] Add configuration validation.
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

- [ ] Improve structured logging.
- [ ] Add agent session IDs.
- [ ] Add pane IDs to logs.
- [ ] Add command IDs.
- [ ] Add execution timing.
- [ ] Add agent token/cost tracking where available.
- [ ] Add debug mode.
- [ ] Add trace mode.
- [ ] Add crash diagnostics.

## 12. Testing

- [ ] Unit tests for layout.
- [ ] Unit tests for VT parser.
- [ ] Unit tests for input handling.
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

- [ ] Architecture documentation.
- [ ] Agent architecture documentation.
- [ ] Agent protocol documentation.
- [ ] Configuration reference.
- [ ] Key binding reference.
- [ ] Plugin/agent integration guide.
- [ ] Developer guide.
- [ ] Security model.
- [ ] Concurrency model.
- [ ] VT emulator documentation.
- [ ] Contribution guide.

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


# file tree 
tatami/
├── AGENTS.md
├── README.md
├── DOCS.md
├── TODO.md
├── LICENSE
├── .gitignore
├── go.mod
├── go.sum
│
├── cmd/
│   └── tatami/
│       └── main.go
│
├── internal/
│   ├── pane/
│   │   ├── pane.go
│   │   ├── input.go
│   │   ├── output.go
│   │   ├── draw.go
│   │   ├── clipboard.go
│   │   ├── process.go
│   │   └── vt.go
│   │
│   ├── layout/
│   │   ├── layout.go
│   │   ├── draw.go
│   │   ├── focus.go
│   │   ├── resize.go
│   │   ├── splits.go
│   │   ├── transform.go
│   │   └── close.go
│   │
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   │
│   ├── format/
│   │   └── format.go
│   │
│   ├── input/
│   │   ├── keys.go
│   │   └── input.go
│   │
│   └── state/
│       └── state.go
│
├── configs/
│   └── tatami.example.conf
│
├── docs/
│   └── architecture.md
│
└── testdata/
    └── vt/


