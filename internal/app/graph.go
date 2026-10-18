// Graph command prompt and result overlay.
//
// The prompt is a modal tcell overlay driven from the main event loop: prefix+:
// opens it, the user types a raw Cypher statement or a ":name" shortcut, Enter
// runs it against FalkorDB and the scrollable result panel opens. prefix+g
// indexes the current working directory into the graph. Everything here runs on
// the main goroutine, so it may touch the screen directly, but queries are
// executed synchronously with a timeout, so a slow FalkorDB briefly blocks the
// UI (acceptable for an interactive command).
package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/atharva-0123-cs/tatami/falkor"
	"github.com/atharva-0123-cs/tatami/pane"
)

// graphModeKind selects which graph overlay (if any) is showing.
type graphModeKind int

const (
	graphOff graphModeKind = iota
	graphInput
	graphResultMode
)

// graphQueryTimeout bounds a single prompt query; graphIndexTimeout bounds a
// full project scan and ingest.
const (
	graphQueryTimeout = 5 * time.Second
	graphIndexTimeout = 120 * time.Second
)

// graphHealthInterval is how often the background monitor probes FalkorDB.
const graphHealthInterval = 10 * time.Second

// graph overlay styles (dark theme, matching the status bar palette).
var (
	graphPanel = tcell.StyleDefault.Background(tcell.Color(235)).Foreground(tcell.Color(252))
	graphBar   = tcell.StyleDefault.Background(tcell.Color(24)).Foreground(tcell.Color(231))
	graphTitle = tcell.StyleDefault.Background(tcell.Color(24)).Foreground(tcell.Color(231)).Bold(true)
)

// openGraphPrompt arms the Cypher input line with an empty buffer.
func (a *App) openGraphPrompt() {
	a.graphMode = graphInput
	a.graphBuf = ""
	a.graphScroll = 0
}

// handleGraphInput processes keys while the Cypher prompt is open. It always
// consumes the event so the focused shell never sees prompt keystrokes.
func (a *App) handleGraphInput(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEsc, tcell.KeyCtrlC:
		a.graphMode = graphOff
	case tcell.KeyEnter:
		q := strings.TrimSpace(a.graphBuf)
		a.graphMode = graphOff
		if q != "" {
			a.runGraphQuery(q)
		}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if r := []rune(a.graphBuf); len(r) > 0 {
			a.graphBuf = string(r[:len(r)-1])
		}
	case tcell.KeyCtrlU:
		a.graphBuf = ""
	case tcell.KeyRune:
		a.graphBuf += string(ev.Rune())
	}
	return true
}

// runGraphQuery resolves builtin shortcuts and executes the statement.
func (a *App) runGraphQuery(input string) {
	if falkor.GlobalClient == nil {
		a.setGraphResult("FalkorDB client is not initialized.")
		return
	}
	cypher, ok := falkor.ResolveBuiltin(input)
	if !ok {
		a.setGraphResult(fmt.Sprintf("Unknown command: %s\n\nBuiltins: %s\nOr type a raw MATCH ... RETURN ... query.",
			input, strings.Join(falkor.BuiltinNames(), ", ")))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), graphQueryTimeout)
	defer cancel()
	res, err := falkor.GlobalClient.Run(ctx, cypher)
	if err != nil {
		a.setGraphResult(fmt.Sprintf("Query failed:\n%v\n\n%s", err, cypher))
		return
	}
	a.setGraphResult(res.Format())
}

// indexProject scans the working directory and writes it to FalkorDB.
func (a *App) indexProject() {
	if falkor.GlobalClient == nil {
		a.SetErrorMsg("FalkorDB client is not initialized")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), graphIndexTimeout)
	defer cancel()
	stats, err := falkor.GlobalClient.IndexProject(ctx, ".")
	if err != nil {
		a.SetErrorMsg(fmt.Sprintf("Index failed: %v", err))
		return
	}
	a.SetErrorMsg(fmt.Sprintf("Indexed %d files → %d nodes, %d edges (%d statements)",
		stats.Files, stats.Nodes, stats.Edges, stats.Statements))
}

// startGraphMonitor launches a background goroutine that periodically probes
// FalkorDB and wakes the UI when connectivity changes. The goroutine never
// touches the screen: it follows the same rule as the PTY readers and only
// calls pane.Interrupt, which posts an EventInterrupt to the main loop.
func (a *App) startGraphMonitor() {
	if falkor.GlobalClient == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.graphCancel = cancel

	go func() {
		client := falkor.GlobalClient
		last := client.Connected()
		ticker := time.NewTicker(graphHealthInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				err := client.HealthCheck(ctx)
				if ctx.Err() != nil {
					return
				}
				if now := err == nil; now != last {
					last = now
					pane.Interrupt()
				}
			}
		}
	}()
}

// graphStatusLabel returns the status-bar health indicator for the graph
// subsystem, or "" when no client is configured.
func (a *App) graphStatusLabel() string {
	c := falkor.GlobalClient
	if c == nil {
		return ""
	}
	if c.Connected() {
		return "graph:on"
	}
	return "graph:off"
}

// setGraphResult shows the scrollable result panel.
func (a *App) setGraphResult(text string) {
	a.graphResult = text
	a.graphScroll = 0
	a.graphMode = graphResultMode
}

// handleGraphResult scrolls or dismisses the result panel.
func (a *App) handleGraphResult(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEsc, tcell.KeyEnter:
		a.graphMode = graphOff
		return true
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			a.graphMode = graphOff
		case 'j', 'n':
			a.graphScroll++
		case 'k', 'p':
			if a.graphScroll > 0 {
				a.graphScroll--
			}
		case 'g':
			a.graphScroll = 0
		}
	}
	return true
}

// renderGraphOverlay paints the active graph overlay over the panes. It must be
// called on the main goroutine after the layout is drawn and before Show.
func (a *App) renderGraphOverlay() {
	switch a.graphMode {
	case graphInput:
		a.drawGraphPrompt()
	case graphResultMode:
		a.drawGraphResult()
	}
}

// drawGraphPrompt paints the input line and a hint row at the bottom.
func (a *App) drawGraphPrompt() {
	w, h := a.screen.Size()
	if h < 3 || w < 12 {
		return
	}
	hint := a.graphHint()
	fillRow(a.screen, w, h-3, graphPanel)
	drawString(a.screen, 0, h-3, w, hint, graphPanel)

	promptRow := h - 2
	fillRow(a.screen, w, promptRow, graphBar)
	text := "Cypher> " + a.graphBuf
	drawString(a.screen, 0, promptRow, w, text, graphBar)
	a.screen.ShowCursor(min(len([]rune(text)), w-1), promptRow)
}

// graphHint returns the shortcut help line shown above the prompt.
func (a *App) graphHint() string {
	return " :" + strings.Join(falkor.BuiltinNames(), "  :") +
		"   | :deps <path>  :callers <fn>  :chain <fn>  | Esc cancel, Enter run"
}

// drawGraphResult paints a scrollable panel with the query output.
func (a *App) drawGraphResult() {
	w, h := a.screen.Size()
	if h < 5 || w < 20 {
		return
	}
	x0, y0, x1, y1 := 1, 1, w-2, h-2
	for y := y0; y <= y1; y++ {
		fillRow(a.screen, w, y, graphPanel)
	}
	drawString(a.screen, x0+1, y0, x1-x0-1, " Graph result ", graphTitle)

	bodyTop, bodyBottom := y0+1, y1-1
	bodyW := x1 - x0 - 1
	bodyH := bodyBottom - bodyTop + 1
	if bodyH < 1 || bodyW < 1 {
		return
	}

	lines := wrapLines(a.graphResult, bodyW)
	maxScroll := len(lines) - bodyH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if a.graphScroll > maxScroll {
		a.graphScroll = maxScroll
	}
	for i := a.graphScroll; i < len(lines) && i-a.graphScroll < bodyH; i++ {
		drawString(a.screen, x0+1, bodyTop+(i-a.graphScroll), bodyW, lines[i], graphPanel)
	}

	hint := "[" + fmt.Sprintf("%d/%d", a.graphScroll+1, maxScroll+1) + "] j/k scroll  q close"
	drawString(a.screen, x1-len([]rune(hint)), y1, len([]rune(hint)), hint, graphTitle)
	a.screen.HideCursor()
}

// fillRow blanks an entire screen row with a style.
func fillRow(screen tcell.Screen, w, y int, st tcell.Style) {
	for x := 0; x < w; x++ {
		screen.SetContent(x, y, ' ', nil, st)
	}
}

// drawString writes s at (x, y), clipping to maxW columns.
func drawString(screen tcell.Screen, x, y, maxW int, s string, st tcell.Style) {
	for i, r := range []rune(s) {
		if i >= maxW {
			return
		}
		screen.SetContent(x+i, y, r, nil, st)
	}
}

// wrapLines hard-wraps text to width, honouring explicit newlines.
func wrapLines(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, raw := range strings.Split(text, "\n") {
		if raw == "" {
			out = append(out, "")
			continue
		}
		for len([]rune(raw)) > width {
			runes := []rune(raw)
			out = append(out, string(runes[:width]))
			raw = string(runes[width:])
		}
		out = append(out, raw)
	}
	return out
}
