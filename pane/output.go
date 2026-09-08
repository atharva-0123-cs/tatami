// PTY output pumping: one goroutine per pane feeds shell output into the
// emulator under the pane mutex and wakes the UI for a render.
package pane

// readPtyOutput pumps PTY output into the emulator under the pane mutex and
// wakes the UI by calling Interrupt(). It returns when the PTY read fails.
//
// One goroutine per pane, started by NewPane. Every read chunk is fed to the
// emulator while holding mu (so no Draw can observe a half-applied chunk), then
// Interrupt posts an EventInterrupt so the main loop renders the update. When
// the shell exits the PTY read fails; readPtyOutput waits for the Wait
// goroutine to close p.done and exits.
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
