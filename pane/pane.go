// Package pane provides the Pane type: a real shell running in a PTY paired
// with a small VT100/xterm-compatible terminal emulator (the term package).
// Each Pane owns one PTY, one goroutine that pumps shell output into the
// emulator (output.go), and one goroutine that waits for the shell process to
// exit; the emulated grid is drawn into the tcell screen by the main goroutine
// (draw.go) and key events are translated for the shell in input.go. Shell
// spawning lives in process.go.
package pane

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atharva-0123-cs/tatami/term"
	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
)

// Dark-theme border colors: xterm 256 palette indices.
const (
	colBorder      = tcell.Color(239) | tcell.ColorValid // dim gray
	colBorderFocus = tcell.Color(51) | tcell.ColorValid  // bright cyan
)

// Interrupt wakes the UI event loop (screen.PollEvent). It is safe to call
// from any goroutine. The application installs a handler during startup.
var Interrupt = func() {}

// Pane wraps a PTY running a shell and a terminal emulator, acting as a single
// panel in the multiplexer.
//
// Concurrency: the emulator grid (term) may only be touched under mu, which
// guards both the PTY-output goroutine (writing) and Draw (reading). Writes to
// the PTY master are serialized by ptyMtx so keystrokes and hot-path writes
// (e.g. bracketed-paste delimiters) never interleave mid-sequence.
type Pane struct {
	name    string
	id      string // stable, process-wide unique identifier
	cwd     string // working directory the shell was started in
	pty     *os.File
	cmd     *exec.Cmd
	ptyMtx  sync.Mutex
	done    chan struct{} // closed when the child process exits
	mu      sync.Mutex
	dead    bool
	exitMsg string
	width   int
	height  int
	x0, y0  int
	x1, y1  int
	term    *term.Terminal // emulated screen
	lg      *slog.Logger   // structured logger tagged with the pane id
}

// nextPaneID hands out monotonically increasing pane identifiers.
var nextPaneID atomic.Uint64

// NewPane creates a new pane and starts a shell in a PTY.
//
// The shell is chosen by shellCandidates (user's $SHELL first), and is started
// in the current working directory with TERM forced to xterm-256color so full
// color support is active regardless of the enclosing terminal. The PTY is
// created at 80x24; the real size is applied on the first Draw pass.
//
// Two goroutines are spawned per pane: readPtyOutput pumps the shell's output
// into the emulator, and a Wait goroutine marks the pane dead when the shell
// exits (closing p.done). NewPane fails only if no candidate shell could be
// started.
func NewPane(label string) (*Pane, error) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}
	return newPane(label, cwd)
}

// NewPaneInDir creates a pane whose shell starts in the given working
// directory. An empty (or unresolvable) directory falls back to the process's
// current directory, so restore never fails on a vanished path.
func NewPaneInDir(label, cwd string) (*Pane, error) {
	if cwd == "" {
		if got, err := os.Getwd(); err == nil {
			cwd = got
		}
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		if got, gerr := os.Getwd(); gerr == nil {
			cwd = got
		}
	}
	return newPane(label, cwd)
}

func newPane(label, cwd string) (*Pane, error) {
	p := &Pane{
		name: "pane-" + label,
		id:   fmt.Sprintf("pane-%d", nextPaneID.Add(1)),
		done: make(chan struct{}),
	}
	p.lg = slog.With("pane", p.label())
	p.cwd = cwd

	master, startCmd, err := spawnShell(cwd)
	if err != nil {
		return nil, err
	}

	p.pty = master
	p.cmd = startCmd
	p.term = term.New(120, 40, func(b []byte) {
		_, _ = p.WriteInput(b)
	})
	p.ptyMtx.Lock()
	_ = pty.Setsize(p.pty, &pty.Winsize{Rows: 40, Cols: 120})
	p.ptyMtx.Unlock()

	go p.readPtyOutput()
	go func() {
		err := p.cmd.Wait()
		if err != nil && err.Error() != "signal: terminated" {
			p.setDead(fmt.Sprintf("exited (%v)", err))
			p.log().Info("pane process exited", "err", err)
		} else {
			p.setDead("exited")
			p.log().Info("pane process exited")
		}
		close(p.done)
	}()

	return p, nil
}

// label returns the pane's display number (name without the "pane-" prefix).
func (p *Pane) label() string { return strings.TrimPrefix(p.name, "pane-") }

// log returns the pane's structured logger, falling back to the global logger
// for panes constructed without one (e.g. zero-value panes in tests).
func (p *Pane) log() *slog.Logger {
	if p.lg == nil {
		return slog.Default()
	}
	return p.lg
}

// MarkDead flags the pane as terminated without waking the UI. It is used by
// tests and tooling to simulate a dead shell.
//
// Unlike setDead it does not call Interrupt, so a MarkDead alone does not
// trigger a render; the main loop discovers the flag on its next CloseDeadPanes
// pass.
func (p *Pane) MarkDead(msg string) {
	p.mu.Lock()
	p.dead = true
	p.exitMsg = msg
	p.mu.Unlock()
}

// setDead marks the pane as terminated and flags it on the border, then wakes
// the UI so the main loop can auto-close the pane. It is called from the
// process-exit waiter goroutine.
func (p *Pane) setDead(msg string) {
	p.MarkDead(msg)
	Interrupt()
}

// Name returns the pane's identifier.
func (p *Pane) Name() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.name
}

// SetName gives the pane a user-visible name (used by pane naming and by agents
// that want a stable label). An empty name is ignored.
func (p *Pane) SetName(name string) {
	if name == "" {
		return
	}
	p.mu.Lock()
	p.name = name
	p.mu.Unlock()
}

// ID returns a stable, process-wide unique identifier for the pane. Unlike the
// display name it never changes and is safe to use as a graph node key or a
// log correlation field.
func (p *Pane) ID() string { return p.id }

// Pid returns the process id of the pane's shell, or 0 when there is none.
func (p *Pane) Pid() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Cwd returns the shell's current working directory. It prefers the live
// directory of the child process (via /proc on Linux) and falls back to the
// directory the shell was started in when that cannot be read.
func (p *Pane) Cwd() string {
	if pid := p.Pid(); pid > 0 {
		if target, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)); err == nil {
			return target
		}
	}
	return p.cwd
}

// ParentPid returns the process id of the shell's parent, read from
// /proc/<pid>/stat, or 0 when it cannot be determined.
func (p *Pane) ParentPid() int {
	pid := p.Pid()
	if pid <= 0 {
		return 0
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	// Format: pid (comm) state ppid ...  comm may contain spaces/parens, so
	// parse from the last ')'.
	s := string(data)
	close := strings.LastIndexByte(s, ')')
	if close < 0 || close+2 >= len(s) {
		return 0
	}
	fields := strings.Fields(s[close+1:])
	if len(fields) < 2 {
		return 0
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0
	}
	return ppid
}

// Title returns the window title set by the shell via OSC 0/2, or "".
func (p *Pane) Title() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return ""
	}
	return p.term.Title()
}

// Text returns the pane's full screen contents (including scrollback) as plain
// text. It is used by the agent harness to read pane output.
func (p *Pane) Text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return ""
	}
	return p.term.AllText()
}

// Scroll sets the copy-mode scroll offset (lines of scrollback shown above the
// screen). A negative value is clamped to 0; an oversized value clamps to the
// scrollback depth.
func (p *Pane) Scroll(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term != nil {
		p.term.SetScrollOffset(n)
	}
}

// ScrollOffset reports the current copy-mode scroll offset.
func (p *Pane) ScrollOffset() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return 0
	}
	return p.term.ScrollOffset()
}

// ScrollbackLen reports how many lines of scrollback the pane retains.
func (p *Pane) ScrollbackLen() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return 0
	}
	return p.term.ScrollbackLen()
}

// Height reports the pane's inner text height in rows (the grid, excluding the
// one-cell border). It is zero until the pane has been drawn once.
func (p *Pane) Height() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.height
}

// IsDead reports whether the underlying process has terminated.
//
// Set by the Wait goroutine (via setDead); the main loop polls this from
// CloseDeadPanes on every event to auto-close exited panes.
func (p *Pane) IsDead() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dead
}

// Close terminates the shell process and closes the PTY. For a pane whose
// process has already exited, only the PTY is closed.
//
// A running shell first gets SIGINT and is given 150 ms to exit; if it is
// still alive after the wait (or sending the signal failed) it is killed. The
// PTY master is then closed, which also unblocks readPtyOutput. Close is
// idempotent with respect to a subsequent double-close because a closed PTY
// fails subsequent writes/reads rather than panicking.
func (p *Pane) Close() error {
	p.log().Info("closing pane")
	if p.cmd != nil && p.cmd.Process != nil && !p.IsDead() {
		if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
			p.log().Error("error sending interrupt to process", "err", err)
			if err := p.cmd.Process.Kill(); err != nil {
				p.log().Error("error killing process", "err", err)
			}
		}
		select {
		case <-p.done:
		case <-time.After(150 * time.Millisecond):
			p.log().Warn("process did not exit within timeout, forcing PTY close")
		}
	}
	if p.pty != nil {
		if err := p.pty.Close(); err != nil {
			p.log().Error("error closing PTY", "err", err)
			p.closeTerm()
			return err
		}
	}
	p.closeTerm()
	return nil
}

// closeTerm shuts down the emulator's responder goroutine.
func (p *Pane) closeTerm() {
	p.mu.Lock()
	term := p.term
	p.mu.Unlock()
	if term != nil {
		term.Close()
	}
}
