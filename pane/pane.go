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
	"github.com/nsf/termbox-go"
)

// Dark-theme border colors: xterm 256 palette index + 1 (termbox attribute).
const (
	colBorder      = termbox.Attribute(240) // palette 239: dim gray
	colBorderFocus = termbox.Attribute(52)  // palette 51: bright cyan
)

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
	termbox.Interrupt()
}

// Draw renders the border and terminal cells onto termbox within [x0, y0, x1, y1).
// Returns the absolute cursor coordinates and whether the cursor is visible.
func (p *Pane) Draw(x0, y0, x1, y1 int, isFocused bool) (cx, cy int, hasCursor bool) {
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

	borderAttr := colBorder
	if isFocused {
		borderAttr = colBorderFocus | termbox.AttrBold
	}

	// Draw corners
	termbox.SetCell(x0, y0, '┌', borderAttr, termbox.ColorDefault)
	termbox.SetCell(x1-1, y0, '┐', borderAttr, termbox.ColorDefault)
	termbox.SetCell(x0, y1-1, '└', borderAttr, termbox.ColorDefault)
	termbox.SetCell(x1-1, y1-1, '┘', borderAttr, termbox.ColorDefault)

	// Top and bottom borders
	for x := x0 + 1; x < x1-1; x++ {
		termbox.SetCell(x, y0, '─', borderAttr, termbox.ColorDefault)
		termbox.SetCell(x, y1-1, '─', borderAttr, termbox.ColorDefault)
	}

	// Left and right borders
	for y := y0 + 1; y < y1-1; y++ {
		termbox.SetCell(x0, y, '│', borderAttr, termbox.ColorDefault)
		termbox.SetCell(x1-1, y, '│', borderAttr, termbox.ColorDefault)
	}

	if p.dead {
		title := " dead "
		if p.exitMsg != "" {
			title = fmt.Sprintf(" %s ", p.exitMsg)
		}
		titleRunes := []rune(title)
		for i, r := range titleRunes {
			if x0+2+i < x1-1 {
				termbox.SetCell(x0+2+i, y0, r, termbox.ColorRed|termbox.AttrBold, termbox.ColorDefault)
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
			fg, bg := cellToTermbox(c)
			ch := c.ch
			if ch == 0 {
				ch = ' '
			}
			termbox.SetCell(innerX+x, innerY+y, ch, fg, bg)
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

// cellToTermbox maps an emulator cell to termbox attributes: default colors map
// to ColorDefault, reverse video swaps fg/bg, and bold/underline become attr bits.
func cellToTermbox(c vtCell) (fg, bg termbox.Attribute) {
	if c.reverse {
		c.fg, c.bg = c.bg, c.fg
	}
	if c.fg == vtDefaultColor {
		fg = termbox.ColorDefault
	} else {
		fg = termbox.Attribute(c.fg + 1)
	}
	if c.bg == vtDefaultColor {
		bg = termbox.ColorDefault
	} else {
		bg = termbox.Attribute(c.bg + 1)
	}
	if c.bold {
		fg |= termbox.AttrBold
	}
	if c.underline {
		fg |= termbox.AttrUnderline
	}
	return fg, bg
}

// HandleEvent forwards a termbox key event to the PTY. Returns true if key data was sent.
func (p *Pane) HandleEvent(ev termbox.Event) bool {
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

// KeyToBytes converts a termbox event into the byte sequence expected by a terminal.
func (p *Pane) KeyToBytes(ev termbox.Event) []byte {
	return KeyToBytes(ev, p.AppCursorKeys())
}

// KeyToBytes converts a termbox event into bytes.
func KeyToBytes(ev termbox.Event, appCursor bool) []byte {
	if ev.Type != termbox.EventKey {
		return nil
	}

	// Printable characters first: termbox reports a plain character with
	// Key == 0, which collides with KeyCtrlSpace (also 0). Checking Ch
	// before the key-code switch keeps letters from being sent as NUL.
	if ev.Ch != 0 {
		if ev.Mod&termbox.ModAlt != 0 {
			return []byte{0x1b, byte(ev.Ch)}
		}
		return []byte(string(ev.Ch))
	}

	arrow := func(csi, ss3 byte) []byte {
		if appCursor {
			return []byte{0x1b, 'O', ss3}
		}
		return []byte{0x1b, '[', csi}
	}

	switch ev.Key {
	case termbox.KeyEnter:
		return []byte{'\r'}
	case termbox.KeyBackspace:
		return []byte{0x7f}
	case termbox.KeyBackspace2:
		return []byte{0x08}
	case termbox.KeyTab:
		return []byte{'\t'}
	case termbox.KeyEsc:
		return []byte{0x1b}
	case termbox.KeySpace:
		return []byte{' '}
	case termbox.KeyArrowUp:
		return arrow('A', 'A')
	case termbox.KeyArrowDown:
		return arrow('B', 'B')
	case termbox.KeyArrowRight:
		return arrow('C', 'C')
	case termbox.KeyArrowLeft:
		return arrow('D', 'D')
	case termbox.KeyHome:
		if appCursor {
			return []byte{0x1b, 'O', 'H'}
		}
		return []byte("\x1b[H")
	case termbox.KeyEnd:
		if appCursor {
			return []byte{0x1b, 'O', 'F'}
		}
		return []byte("\x1b[F")
	case termbox.KeyPgup:
		return []byte("\x1b[5~")
	case termbox.KeyPgdn:
		return []byte("\x1b[6~")
	case termbox.KeyDelete:
		return []byte("\x1b[3~")
	case termbox.KeyInsert:
		return []byte("\x1b[2~")
	case termbox.KeyF1:
		return []byte("\x1bOP")
	case termbox.KeyF2:
		return []byte("\x1bOQ")
	case termbox.KeyF3:
		return []byte("\x1bOR")
	case termbox.KeyF4:
		return []byte("\x1bOS")
	case termbox.KeyF5:
		return []byte("\x1b[15~")
	case termbox.KeyF6:
		return []byte("\x1b[17~")
	case termbox.KeyF7:
		return []byte("\x1b[18~")
	case termbox.KeyF8:
		return []byte("\x1b[19~")
	case termbox.KeyF9:
		return []byte("\x1b[20~")
	case termbox.KeyF10:
		return []byte("\x1b[21~")
	case termbox.KeyF11:
		return []byte("\x1b[23~")
	case termbox.KeyF12:
		return []byte("\x1b[24~")
	case termbox.KeyCtrlSpace:
		return []byte{0}
	case termbox.KeyCtrlA:
		return []byte{1}
	case termbox.KeyCtrlB:
		return []byte{2}
	case termbox.KeyCtrlC:
		return []byte{3}
	case termbox.KeyCtrlD:
		return []byte{4}
	case termbox.KeyCtrlE:
		return []byte{5}
	case termbox.KeyCtrlF:
		return []byte{6}
	case termbox.KeyCtrlG:
		return []byte{7}
	case termbox.KeyCtrlJ:
		return []byte{10}
	case termbox.KeyCtrlK:
		return []byte{11}
	case termbox.KeyCtrlL:
		return []byte{12}
	case termbox.KeyCtrlN:
		return []byte{14}
	case termbox.KeyCtrlO:
		return []byte{15}
	case termbox.KeyCtrlP:
		return []byte{16}
	case termbox.KeyCtrlQ:
		return []byte{17}
	case termbox.KeyCtrlR:
		return []byte{18}
	case termbox.KeyCtrlS:
		return []byte{19}
	case termbox.KeyCtrlT:
		return []byte{20}
	case termbox.KeyCtrlU:
		return []byte{21}
	case termbox.KeyCtrlV:
		return []byte{22}
	case termbox.KeyCtrlW:
		return []byte{23}
	case termbox.KeyCtrlX:
		return []byte{24}
	case termbox.KeyCtrlY:
		return []byte{25}
	case termbox.KeyCtrlZ:
		return []byte{26}
	case termbox.KeyCtrl4:
		return []byte{0x1c}
	case termbox.KeyCtrl5:
		return []byte{0x1d}
	case termbox.KeyCtrl6:
		return []byte{0x1e}
	case termbox.KeyCtrl7:
		return []byte{0x1f}
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
// wakes the UI with termbox.Interrupt(). It returns when the PTY read fails.
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
			termbox.Interrupt()
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
