package pane

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

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
type Pane struct {
	name    string
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
	term    *terminal // emulated screen
}

// NewPane creates a new pane and starts a shell in a PTY.
func NewPane(label string) (*Pane, error) {
	p := &Pane{
		name: "pane-" + label,
		done: make(chan struct{}),
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
		log.Printf("Warning: could not get working directory, defaulting to /: %v", err)
	}

	shells := shellCandidates()

	var (
		master   *os.File
		startCmd *exec.Cmd
		lastErr  error
		started  bool
	)
	for _, sh := range shells {
		cmd := exec.Command(sh)
		cmd.Dir = cwd
		cmd.Env = setEnv(os.Environ(), "TERM", "xterm-256color")
		m, serr := pty.Start(cmd)
		if serr == nil {
			master = m
			startCmd = cmd
			started = true
			break
		}
		lastErr = serr
		log.Printf("Warning: failed to start shell %q: %v", sh, serr)
	}

	if !started {
		return nil, fmt.Errorf(
			"failed to start any shell (tried %s): %w. Ensure a login shell is available and that PTY creation is permitted in this environment",
			strings.Join(shells, ", "), lastErr,
		)
	}

	p.pty = master
	p.cmd = startCmd
	p.term = newTerminal(80, 24, func(b []byte) {
		_, _ = p.WriteInput(b)
	})

	go p.readPtyOutput()
	go func() {
		err := p.cmd.Wait()
		if err != nil && err.Error() != "signal: terminated" {
			p.setDead(fmt.Sprintf("exited (%v)", err))
			log.Printf("Pane %s process exited with: %v", p.label(), err)
		} else {
			p.setDead("exited")
			log.Printf("Pane %s process exited.", p.label())
		}
		close(p.done)
	}()

	return p, nil
}

// label returns the pane's display number (name without the "pane-" prefix).
func (p *Pane) label() string { return strings.TrimPrefix(p.name, "pane-") }

// shellCandidates returns the shells to try, preferring $SHELL, deduped and
// verified to exist in $PATH.
func shellCandidates() []string {
	var candidates []string
	seen := map[string]bool{}
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		if _, err := exec.LookPath(s); err != nil {
			log.Printf("Shell candidate %q not found, skipping: %v", s, err)
			return
		}
		seen[s] = true
		candidates = append(candidates, s)
	}
	add(os.Getenv("SHELL"))
	add("bash")
	add("zsh")
	add("sh")
	return candidates
}

// setEnv returns env with key=val set, replacing any existing entry instead of
// appending a duplicate.
func setEnv(env []string, key, val string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}

// MarkDead flags the pane as terminated without waking the UI. It is used by
// tests and tooling to simulate a dead shell.
func (p *Pane) MarkDead(msg string) {
	p.mu.Lock()
	p.dead = true
	p.exitMsg = msg
	p.mu.Unlock()
}

// setDead marks the pane as terminated and flags it on the border.
func (p *Pane) setDead(msg string) {
	p.MarkDead(msg)
	Interrupt()
}

// Draw renders the border and terminal cells onto the screen within
// [x0, y0, x1, y1). Returns the absolute cursor coordinates and whether the
// cursor is visible.
func (p *Pane) Draw(x0, y0, x1, y1 int, isFocused bool, screen tcell.Screen) (cx, cy int, hasCursor bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.x0, p.y0, p.x1, p.y1 = x0, y0, x1, y1

	w := x1 - x0 - 2
	if w < 1 {
		w = 1
	}
	h := y1 - y0 - 2
	if h < 1 {
		h = 1
	}

	if w != p.width || h != p.height {
		p.width = w
		p.height = h
		p.term.resize(w, h)
		p.ptyMtx.Lock()
		if p.pty != nil {
			if err := pty.Setsize(p.pty, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}); err != nil {
				log.Printf("Error setting PTY size for pane %s: %v", p.label(), err)
			}
		}
		p.ptyMtx.Unlock()
	}

	borderStyle := tcell.StyleDefault.Foreground(colBorder)
	if isFocused {
		borderStyle = tcell.StyleDefault.Foreground(colBorderFocus).Bold(true)
	}

	// Draw corners
	screen.SetContent(x0, y0, '┌', nil, borderStyle)
	screen.SetContent(x1-1, y0, '┐', nil, borderStyle)
	screen.SetContent(x0, y1-1, '└', nil, borderStyle)
	screen.SetContent(x1-1, y1-1, '┘', nil, borderStyle)

	// Top and bottom borders
	for x := x0 + 1; x < x1-1; x++ {
		screen.SetContent(x, y0, '─', nil, borderStyle)
		screen.SetContent(x, y1-1, '─', nil, borderStyle)
	}

	// Left and right borders
	for y := y0 + 1; y < y1-1; y++ {
		screen.SetContent(x0, y, '│', nil, borderStyle)
		screen.SetContent(x1-1, y, '│', nil, borderStyle)
	}

	if p.dead {
		title := " dead "
		if p.exitMsg != "" {
			title = fmt.Sprintf(" %s ", p.exitMsg)
		}
		deadStyle := tcell.StyleDefault.Foreground(tcell.PaletteColor(1)).Bold(true)
		for i, r := range []rune(title) {
			if x0+2+i < x1-1 {
				screen.SetContent(x0+2+i, y0, r, nil, deadStyle)
			}
		}
	}

	innerX := x0 + 1
	innerY := y0 + 1

	for y := 0; y < h && y < p.term.rows; y++ {
		for x := 0; x < w && x < p.term.cols; x++ {
			c := *p.term.at(x, y)
			if c.wide {
				continue // right half of a double-width rune drawn at x-1
			}
			ch := c.ch
			if ch == 0 {
				ch = ' '
			}
			screen.SetContent(innerX+x, innerY+y, ch, nil, cellToStyle(c))
		}
	}

	if isFocused && p.term.cursorVisible {
		cx = innerX + p.term.x
		cy = innerY + p.term.y
		if cx >= innerX && cx < innerX+w && cy >= innerY && cy < innerY+h {
			hasCursor = true
		}
	}

	return cx, cy, hasCursor
}

// cellToStyle maps an emulator cell to a tcell style. Reverse video swaps
// fg/bg, default colors map to ColorDefault, palette cells carry their index,
// and truecolor cells carry their original RGB components; bold and underline
// become style attributes.
func cellToStyle(c vtCell) tcell.Style {
	if c.reverse {
		c.fg, c.bg = c.bg, c.fg
	}
	st := tcell.StyleDefault
	if c.fg.def {
		st = st.Foreground(tcell.ColorDefault)
	} else if c.fg.rgb {
		st = st.Foreground(tcell.NewRGBColor(int32(c.fg.r), int32(c.fg.g), int32(c.fg.b)))
	} else {
		st = st.Foreground(tcell.PaletteColor(c.fg.idx))
	}
	if c.bg.def {
		st = st.Background(tcell.ColorDefault)
	} else if c.bg.rgb {
		st = st.Background(tcell.NewRGBColor(int32(c.bg.r), int32(c.bg.g), int32(c.bg.b)))
	} else {
		st = st.Background(tcell.PaletteColor(c.bg.idx))
	}
	if c.bold {
		st = st.Bold(true)
	}
	if c.underline {
		st = st.Underline(true)
	}
	return st
}

// HandleEvent forwards a tcell key event to the PTY. Returns true if key data was sent.
func (p *Pane) HandleEvent(ev *tcell.EventKey) bool {
	data := p.KeyToBytes(ev)
	if len(data) > 0 {
		if _, err := p.WriteInput(data); err != nil {
			log.Printf("Error writing input to PTY for pane %s: %v", p.label(), err)
		}
		return true
	}
	return false
}

// AppCursorKeys reports whether application cursor-key mode (DECCKM) is active.
func (p *Pane) AppCursorKeys() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.term != nil && p.term.decckm
}

// CopyText copies the pane's scrollback + screen to the system clipboard.
func (p *Pane) CopyText() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return "", fmt.Errorf("pane not ready")
	}
	text := p.term.allText()
	if text == "" {
		return "nothing to copy", nil
	}
	lines := strings.Count(text, "\n") + 1
	tool, err := CopyToClipboard(text)
	if err != nil {
		return "", err
	}
	if tool == "" {
		return fmt.Sprintf("copied %d lines", lines), nil
	}
	return fmt.Sprintf("copied %d lines via %s", lines, tool), nil
}

// Paste reports whether the pane's shell has enabled bracketed paste
// (DECSET 2004), so the UI should wrap pasted input in ESC[200~/ESC[201~.
func (p *Pane) Paste() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.term != nil && p.term.bracketedPaste
}

// KeyToBytes converts a tcell event into the byte sequence expected by a terminal.
func (p *Pane) KeyToBytes(ev *tcell.EventKey) []byte {
	return KeyToBytes(ev, p.AppCursorKeys())
}

// KeyToBytes converts a tcell event into bytes.
func KeyToBytes(ev *tcell.EventKey, appCursor bool) []byte {
	if ev == nil {
		return nil
	}

	// Plain printable characters come through as KeyRune with the rune set.
	if ev.Key() == tcell.KeyRune {
		r := ev.Rune()
		if r == 0 {
			return nil
		}
		if ev.Modifiers()&tcell.ModAlt != 0 {
			return []byte{0x1b, byte(r)}
		}
		return []byte(string(r))
	}

	arrow := func(csi, ss3 byte) []byte {
		if appCursor {
			return []byte{0x1b, 'O', ss3}
		}
		return []byte{0x1b, '[', csi}
	}

	switch ev.Key() {
	case tcell.KeyEnter:
		return []byte{'\r'}
	case tcell.KeyBackspace:
		return []byte{0x7f}
	case tcell.KeyTab:
		return []byte{'\t'}
	case tcell.KeyEsc:
		return []byte{0x1b}
	case tcell.KeyUp:
		return arrow('A', 'A')
	case tcell.KeyDown:
		return arrow('B', 'B')
	case tcell.KeyRight:
		return arrow('C', 'C')
	case tcell.KeyLeft:
		return arrow('D', 'D')
	case tcell.KeyHome:
		if appCursor {
			return []byte{0x1b, 'O', 'H'}
		}
		return []byte("\x1b[H")
	case tcell.KeyEnd:
		if appCursor {
			return []byte{0x1b, 'O', 'F'}
		}
		return []byte("\x1b[F")
	case tcell.KeyPgUp:
		return []byte("\x1b[5~")
	case tcell.KeyPgDn:
		return []byte("\x1b[6~")
	case tcell.KeyDelete:
		return []byte("\x1b[3~")
	case tcell.KeyInsert:
		return []byte("\x1b[2~")
	case tcell.KeyF1:
		return []byte("\x1bOP")
	case tcell.KeyF2:
		return []byte("\x1bOQ")
	case tcell.KeyF3:
		return []byte("\x1bOR")
	case tcell.KeyF4:
		return []byte("\x1bOS")
	case tcell.KeyF5:
		return []byte("\x1b[15~")
	case tcell.KeyF6:
		return []byte("\x1b[17~")
	case tcell.KeyF7:
		return []byte("\x1b[18~")
	case tcell.KeyF8:
		return []byte("\x1b[19~")
	case tcell.KeyF9:
		return []byte("\x1b[20~")
	case tcell.KeyF10:
		return []byte("\x1b[21~")
	case tcell.KeyF11:
		return []byte("\x1b[23~")
	case tcell.KeyF12:
		return []byte("\x1b[24~")
	case tcell.KeyCtrlSpace:
		return []byte{0}
	case tcell.KeyCtrlBackslash: // Ctrl-\
		return []byte{0x1c}
	case tcell.KeyCtrlRightSq: // Ctrl-]
		return []byte{0x1d}
	case tcell.KeyCtrlCarat: // Ctrl-^
		return []byte{0x1e}
	case tcell.KeyCtrlUnderscore: // Ctrl-_
		return []byte{0x1f}
	}

	// Ctrl+A..Ctrl+Z map to bytes 1..26 (KeyCtrlA == 65).
	if ev.Key() >= tcell.KeyCtrlA && ev.Key() <= tcell.KeyCtrlZ {
		return []byte{byte(ev.Key() - tcell.KeyCtrlA + 1)}
	}

	return nil
}

// WriteInput writes raw bytes to the PTY master.
func (p *Pane) WriteInput(data []byte) (int, error) {
	p.ptyMtx.Lock()
	defer p.ptyMtx.Unlock()
	if p.pty == nil {
		return 0, fmt.Errorf("pty closed")
	}
	return p.pty.Write(data)
}

// readPtyOutput pumps PTY output into the emulator under the pane mutex and
// wakes the UI by calling Interrupt(). It returns when the PTY read fails.
func (p *Pane) readPtyOutput() {
	buf := make([]byte, 4096)
	for {
		n, err := p.pty.Read(buf)
		if err != nil {
			<-p.done
			return
		}
		if n > 0 {
			p.mu.Lock()
			if p.term != nil {
				p.term.process(buf[:n])
			}
			p.mu.Unlock()
			Interrupt()
		}
	}
}

// Name returns the pane's identifier.
func (p *Pane) Name() string { return p.name }

// IsDead reports whether the underlying process has terminated.
func (p *Pane) IsDead() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dead
}

// Close terminates the shell process and closes the PTY. For a pane whose
// process has already exited, only the PTY is closed.
func (p *Pane) Close() error {
	log.Printf("Closing pane %s", p.label())
	if p.cmd != nil && p.cmd.Process != nil && !p.IsDead() {
		if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
			log.Printf("Error sending interrupt to pane %s process: %v", p.label(), err)
			if err := p.cmd.Process.Kill(); err != nil {
				log.Printf("Error killing pane %s process: %v", p.label(), err)
			}
		}
		select {
		case <-p.done:
		case <-time.After(150 * time.Millisecond):
			log.Printf("Warning: pane %s process did not exit within timeout, forcing PTY close", p.label())
		}
	}
	if p.pty != nil {
		if err := p.pty.Close(); err != nil {
			log.Printf("Error closing PTY for pane %s: %v", p.label(), err)
			return err
		}
	}
	return nil
}
