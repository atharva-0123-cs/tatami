package pane

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Pane represents a single terminal pane with a PTY and a tview.TextView
type Pane struct {
	*tview.TextView
	pty     *os.File
	cmd     *exec.Cmd
	app     *tview.Application
	label   string
	ptyMtx  sync.Mutex // Mutex for PTY writes
}

// NewPane creates and initializes a new Pane
func NewPane(app *tview.Application, label string) (*Pane, error) {
	p := &Pane{
		TextView: tview.NewTextView().SetDynamicColors(true).SetRegions(true).SetWordWrap(false).SetMaxLines(1000),
		app:      app,
		label:    label,
	}

	p.SetBorder(true).SetTitle(" [" + label + "] ")

	// Spawn a shell
	cmd := exec.Command("bash") // You can make this configurable (e.g., zsh, fish)
	master, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start pty: %w", err)
	}

	p.pty = master
	p.cmd = cmd

	// Start reading from PTY and writing to TextView
	go p.readPtyOutput()

	// Capture key inputs when focused and send to PTY
	p.SetInputCapture(p.handleInput)

	// Handle TextView resizing to update PTY size
	p.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		// Inner rect of a bordered box.
		ix, iy := x+1, y+1
		iw, ih := width-2, height-2
		if iw > 0 && ih > 0 {
			p.ptyMtx.Lock()
			pty.Setsize(p.pty, &pty.Winsize{Rows: uint16(ih), Cols: uint16(iw)})
			p.ptyMtx.Unlock()
		}
		return ix, iy, iw, ih
	})

	return p, nil
}

// readPtyOutput reads from the PTY and writes to the TextView
	func (p *Pane) readPtyOutput() {
		buf := make([]byte, 1024)
		ansiWriter := tview.ANSIWriter(p.TextView)
		for {
			n, err := p.pty.Read(buf)
			if err != nil {
				log.Printf("Error reading from PTY for pane %s: %v", p.label, err)
				// Check if the command has exited
				if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
					log.Printf("Pane %s process exited.", p.label)
					p.app.QueueUpdateDraw(func() {
						p.SetTitle(" [" + p.label + "] [red]DEAD[white] ")
						// Inform the layout manager that this pane is dead (not implemented yet)
					})
				} else if err != io.EOF { // Only stop on EOF if process hasn't exited
					p.app.QueueUpdateDraw(func() {
						p.Write([]byte(fmt.Sprintf("\n[red]PTY Read Error: %v[white]\n", err)))
					})
				}
				return
			}
			if n > 0 {
				// Write output to the TextView via ANSIWriter, then queue a redraw
				p.app.QueueUpdateDraw(func() {
					ansiWriter.Write(buf[:n])
				})
			}
		}
	}

// WriteInput writes data to the PTY's stdin
func (p *Pane) WriteInput(data []byte) (int, error) {
	p.ptyMtx.Lock()
	defer p.ptyMtx.Unlock()
	return p.pty.Write(data)
}

// handleInput handles key presses when the pane is focused, forwarding them to the PTY
func (p *Pane) handleInput(event *tcell.EventKey) *tcell.EventKey {
	var data []byte
	switch event.Key() {
	case tcell.KeyRune:
		data = []byte(string(event.Rune()))
	case tcell.KeyEnter:
		data = []byte{'\n'}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		data = []byte{0x7f}
	case tcell.KeyTab:
		data = []byte{'\t'}
	case tcell.KeyEscape:
		data = []byte{0x1b}
	case tcell.KeyUp:
		data = []byte("\x1b[A")
	case tcell.KeyDown:
		data = []byte("\x1b[B")
	case tcell.KeyRight:
		data = []byte("\x1b[C")
	case tcell.KeyLeft:
		data = []byte("\x1b[D")
	default:
		// Check for Control characters Ctrl+A through Ctrl+Z
		if event.Key() >= tcell.KeyCtrlA && event.Key() <= tcell.KeyCtrlZ {
			data = []byte{byte(event.Key())}
		}
	}

	if len(data) > 0 {
		_, err := p.WriteInput(data)
		if err != nil {
			log.Printf("Error writing input to PTY for pane %s: %v", p.label, err)
		}
		return nil // Consume event
	}
	return event
}

// Close closes the PTY and kills the command
func (p *Pane) Close() error {
	log.Printf("Closing pane %s", p.label)
	if p.cmd != nil && p.cmd.Process != nil {
		// Send SIGHUP to the process group to ensure child processes are also killed
		err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP)
		if err != nil {
			log.Printf("Error sending SIGHUP to pane %s process: %v", p.label, err)
			// Fallback to Kill if SIGHUP fails or for good measure
			p.cmd.Process.Kill()
		}
		// Wait for the process to exit
		p.cmd.Wait()
	}
	if p.pty != nil {
		return p.pty.Close()
	}
	return nil
}
