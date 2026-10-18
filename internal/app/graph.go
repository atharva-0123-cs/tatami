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

// graphActionKind selects what Enter does in the graph input prompt.
type graphActionKind int

const (
	graphActQuery graphActionKind = iota
	graphActSave
	graphActRestore
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

	graphBorder    = tcell.StyleDefault.Background(tcell.Color(24)).Foreground(tcell.Color(244))
	graphPromptSt  = tcell.StyleDefault.Background(tcell.Color(235)).Foreground(tcell.Color(81)).Bold(true)
	graphInputSt   = tcell.StyleDefault.Background(tcell.Color(235)).Foreground(tcell.Color(231))
	graphCandidate = tcell.StyleDefault.Background(tcell.Color(235)).Foreground(tcell.Color(245))
	graphSelected  = tcell.StyleDefault.Background(tcell.Color(24)).Foreground(tcell.Color(231)).Bold(true)
	graphHintSt    = tcell.StyleDefault.Background(tcell.Color(235)).Foreground(tcell.Color(243))
)

// cypherKeywords are completed in any token position, alongside node labels and
// relationship types.
var cypherKeywords = []string{
	"MATCH", "OPTIONAL", "RETURN", "WHERE", "CREATE", "MERGE", "DELETE",
	"DETACH", "SET", "WITH", "ORDER", "BY", "LIMIT", "SKIP", "AS",
	"COUNT", "DISTINCT", "AND", "OR", "NOT", "IN", "CONTAINS", "STARTS",
	"ENDS", "ASC", "DESC", "UNION", "UNWIND", "EXISTS",
}

// completionWords is the pool for non-command tokens: Cypher keywords plus the
// graph schema's labels and relationship types. completionFirst additionally
// offers the ":name" builtins while the first token is being typed.
var (
	completionWords = buildCompletionWords()
	completionFirst = buildCompletionFirst()
)

func buildCompletionWords() []string {
	out := append([]string{}, cypherKeywords...)
	out = append(out, falkor.SchemaTokens()...)
	return out
}

func buildCompletionFirst() []string {
	out := append([]string{}, completionWords...)
	for _, name := range falkor.BuiltinNames() {
		out = append(out, ":"+name)
	}
	return out
}

// openGraphPrompt arms the Cypher input line with an empty buffer.
func (a *App) openGraphPrompt() {
	a.graphMode = graphInput
	a.graphAction = graphActQuery
	a.graphBuf = ""
	a.graphScroll = 0
	a.resetGraphCompletion()
}

// openSessionPrompt arms the input line for a session save or restore.
func (a *App) openSessionPrompt(action graphActionKind) {
	a.graphMode = graphInput
	a.graphAction = action
	a.graphBuf = "default"
	a.graphScroll = 0
	a.resetGraphCompletion()
}

// handleGraphInput processes keys while the Cypher prompt is open. It always
// consumes the event so the focused shell never sees prompt keystrokes.
func (a *App) handleGraphInput(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEsc, tcell.KeyCtrlC:
		a.graphMode = graphOff
	case tcell.KeyEnter:
		q := strings.TrimSpace(a.graphBuf)
		action := a.graphAction
		a.graphMode = graphOff
		switch {
		case action == graphActSave:
			a.saveSession(q)
		case action == graphActRestore:
			a.restoreSession(q)
		case q != "":
			a.runGraphQuery(q)
		}
	case tcell.KeyTab:
		a.completeGraph(1)
	case tcell.KeyBacktab:
		a.completeGraph(-1)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if r := []rune(a.graphBuf); len(r) > 0 {
			a.graphBuf = string(r[:len(r)-1])
		}
		a.resetGraphCompletion()
	case tcell.KeyCtrlU:
		a.graphBuf = ""
		a.resetGraphCompletion()
	case tcell.KeyRune:
		a.graphBuf += string(ev.Rune())
		a.resetGraphCompletion()
	}
	return true
}

// resetGraphCompletion abandons the active completion cycle. It is called after
// any edit so the next TAB recomputes candidates from the current buffer.
func (a *App) resetGraphCompletion() {
	a.graphCompActive = false
	a.graphCompHead = ""
	a.graphCompItems = nil
	a.graphCompIdx = 0
}

// completeGraph applies the next (dir +1) or previous (dir -1) completion to
// the token at the cursor. The first press captures the candidate list; later
// presses cycle it. A no-op when there are no candidates.
func (a *App) completeGraph(dir int) {
	if !a.graphCompActive {
		items := a.currentCompletions()
		if len(items) == 0 {
			return
		}
		prefix := graphWordPrefix(a.graphBuf)
		a.graphCompHead = a.graphBuf[:len(a.graphBuf)-len(prefix)]
		a.graphCompItems = items
		a.graphCompIdx = 0
		a.graphCompActive = true
	} else {
		n := len(a.graphCompItems)
		a.graphCompIdx = (a.graphCompIdx + dir%n + n) % n
	}
	a.graphBuf = a.graphCompHead + a.graphCompItems[a.graphCompIdx]
}

// currentCompletions returns the candidates for the token at the end of the
// buffer. In the first token (no preceding whitespace) the ":name" builtins are
// offered too; otherwise the pool is Cypher keywords, labels and relationship
// types. Matching is case-insensitive.
func (a *App) currentCompletions() []string {
	prefix := graphWordPrefix(a.graphBuf)
	if prefix == "" {
		return nil
	}
	head := a.graphBuf[:len(a.graphBuf)-len(prefix)]
	pool := completionWords
	if !strings.ContainsAny(head, " \t") {
		pool = completionFirst
	}
	lp := strings.ToLower(prefix)
	var out []string
	for _, cand := range pool {
		if cand != prefix && strings.HasPrefix(strings.ToLower(cand), lp) {
			out = append(out, cand)
		}
	}
	return out
}

// graphWordPrefix returns the trailing run of identifier characters (letters,
// digits, '_', ':' and '-') that completion should replace.
func graphWordPrefix(buf string) string {
	i := len(buf)
	for i > 0 && isGraphWordByte(buf[i-1]) {
		i--
	}
	return buf[i:]
}

func isGraphWordByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_' || c == ':' || c == '-':
		return true
	}
	return false
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

// saveSession snapshots the live workspace and persists it to FalkorDB under
// the given name (empty means "default").
func (a *App) saveSession(name string) {
	if falkor.GlobalClient == nil {
		a.SetErrorMsg("FalkorDB client is not initialized")
		return
	}
	if name == "" {
		name = "default"
	}
	spec := a.wm.Snapshot(name)
	a.forceWriteLiveState()
	ctx, cancel := context.WithTimeout(context.Background(), graphQueryTimeout)
	defer cancel()
	if err := falkor.SaveSession(ctx, falkor.GlobalClient, spec); err != nil {
		a.SetErrorMsg(fmt.Sprintf("Save failed: %v", err))
		return
	}
	a.SetErrorMsg(fmt.Sprintf("Saved session %q: %d window(s), %d pane(s)",
		name, len(spec.Windows), a.wm.PaneCount()))
}

// restoreSession loads a named session from FalkorDB and rebuilds the workspace.
func (a *App) restoreSession(name string) {
	if falkor.GlobalClient == nil {
		a.SetErrorMsg("FalkorDB client is not initialized")
		return
	}
	if name == "" {
		name = "default"
	}
	ctx, cancel := context.WithTimeout(context.Background(), graphQueryTimeout)
	defer cancel()
	spec, err := falkor.RestoreSession(ctx, falkor.GlobalClient, name)
	if err != nil {
		a.SetErrorMsg(fmt.Sprintf("Restore failed: %v", err))
		return
	}
	if err := a.wm.Restore(spec); err != nil {
		a.SetErrorMsg(fmt.Sprintf("Restore failed: %v", err))
		return
	}
	a.SetErrorMsg(fmt.Sprintf("Restored session %q: %d window(s), %d pane(s)",
		name, len(spec.Windows), a.wm.PaneCount()))
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

// setGraphResult shows the scrollable result panel under the default title.
func (a *App) setGraphResult(text string) {
	a.setOverlay("Graph result", text)
}

// setOverlay shows the scrollable result panel with an explicit title.
func (a *App) setOverlay(title, text string) {
	a.overlayTitle = title
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
	if a.pickKind != pickNone {
		a.renderPickerOverlay()
		return
	}
	if a.cmdMode {
		a.renderCommandOverlay()
		return
	}
	switch a.graphMode {
	case graphInput:
		a.drawGraphPrompt()
	case graphResultMode:
		a.drawGraphResult()
	}
}

// maxCompletionRows bounds the completion dropdown height.
const maxCompletionRows = 8

// drawGraphPrompt paints a bordered input panel with an optional completion
// dropdown as the bottom rows of the screen:
//
//	─ Cypher ─────────────────────────────
//	 Cypher> MATCH (f:File)
//	   ❯ File
//	     Function
//	 Tab complete · Enter run · Esc cancel
func (a *App) drawGraphPrompt() {
	w, h := a.screen.Size()
	if h < 2 || w < 12 {
		return
	}

	items, selected := a.visibleCompletions()
	listN := len(items)
	if listN > maxCompletionRows {
		listN = maxCompletionRows
	}
	panelH := 3 + listN
	if panelH > h {
		panelH = h
		listN = h - 3
		if listN < 0 {
			listN = 0
		}
		panelH = 3 + listN
		if panelH > h {
			panelH = h
		}
	}
	top := h - panelH

	for y := top; y < h; y++ {
		fillRow(a.screen, w, y, graphPanel)
	}

	// Titled top border.
	for x := 0; x < w; x++ {
		a.screen.SetContent(x, top, '─', nil, graphBorder)
	}
	drawString(a.screen, 2, top, w-3, " "+a.graphTitleText()+" ", graphTitle)

	// Input line with a colored prompt.
	inputRow := top + 1
	label := a.graphPromptLabel()
	drawString(a.screen, 0, inputRow, w, " "+label, graphPromptSt)
	bufX := 1 + len([]rune(label))
	drawString(a.screen, bufX, inputRow, w-bufX, a.graphBuf, graphInputSt)
	cursorX := bufX + len([]rune(a.graphBuf))
	if cursorX > w-1 {
		cursorX = w - 1
	}
	a.screen.ShowCursor(cursorX, inputRow)

	// Completion dropdown, scrolled so the selection stays visible.
	start := 0
	if selected >= listN && listN > 0 {
		start = selected - listN + 1
	}
	for i := 0; i < listN; i++ {
		idx := start + i
		row := top + 2 + i
		st := graphCandidate
		text := "     " + items[idx]
		if idx == selected {
			st = graphSelected
			text = "   ❯ " + items[idx]
		}
		fillRow(a.screen, w, row, st)
		drawString(a.screen, 0, row, w, text, st)
	}

	// Hint row at the very bottom (omitted when the screen is too short).
	if hintRow := h - 1; hintRow > inputRow {
		hint := " Tab complete · Enter run · Esc cancel"
		if len(items) > 0 {
			if selected >= 0 {
				hint = fmt.Sprintf(" %d/%d · Tab cycle · Enter run · Esc cancel", selected+1, len(items))
			} else {
				hint = fmt.Sprintf(" %d matches · Tab complete · Enter run · Esc cancel", len(items))
			}
		}
		drawString(a.screen, 0, hintRow, w, hint, graphHintSt)
	}
}

// visibleCompletions returns the dropdown items and the highlighted index (-1
// when nothing is selected yet). While cycling it shows the captured list so the
// dropdown stays stable across TAB presses.
func (a *App) visibleCompletions() ([]string, int) {
	if a.graphCompActive {
		return a.graphCompItems, a.graphCompIdx
	}
	return a.currentCompletions(), -1
}

// graphTitleText names the prompt panel for the active input action.
func (a *App) graphTitleText() string {
	switch a.graphAction {
	case graphActSave:
		return "Save session"
	case graphActRestore:
		return "Restore session"
	default:
		return "Cypher"
	}
}

// graphPromptLabel returns the prompt prefix for the active input action.
func (a *App) graphPromptLabel() string {
	switch a.graphAction {
	case graphActSave:
		return "Session name to save> "
	case graphActRestore:
		return "Session name to restore> "
	default:
		return "Cypher> "
	}
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
	title := a.overlayTitle
	if title == "" {
		title = "Graph result"
	}
	drawString(a.screen, x0+1, y0, x1-x0-1, " "+title+" ", graphTitle)

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
