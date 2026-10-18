// Command mode: a modal prompt (prefix+:) with a parser, history, tab
// completion, aliases and help. Commands drive the multiplexer itself (splits,
// windows, panes) and the built-in agent harness (environment/project/git
// context, running commands, reading and driving panes). Everything runs on the
// main goroutine; long agent commands run with a bounded timeout.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/atharva-0123-cs/tatami/agent"
)

// commandTimeout bounds a single agent command run from the prompt.
const commandTimeout = 60 * time.Second

// commandAliases maps short aliases to canonical command names.
var commandAliases = map[string]string{
	"sp": "split", "vs": "vsplit", "nw": "new-window", "n": "next-window",
	"pw": "prev-window", "ls": "list-panes", "lsw": "list-windows",
	"sel": "select-pane", "kill": "close-pane", "rm": "close-pane",
	"sh": "run", "exec": "run", "q": "quit", "?": "help",
	"agents": "agent-list", "as": "agent-start", "ar": "agent-run",
	"ac": "agent-switch", "astat": "agent-status",
}

// commandNames are the canonical commands, used for completion and help.
var commandNames = []string{
	"help", "quit", "split", "vsplit", "new-window", "rename-window",
	"next-window", "prev-window", "list-windows", "list-panes", "select-pane",
	"rename-pane", "close-pane", "send", "run", "resize", "env", "project",
	"git", "agent-context", "agent-start", "agent-list", "agent-switch",
	"agent-status", "agent-run", "agent-stop", "choose-pane", "find",
	"synchronize-panes", "cypher",
}

// commandHelp is the one-line help for each command.
var commandHelp = map[string]string{
	"help":              "show this help",
	"quit":              "quit tatami",
	"split":             "split the focused pane horizontally (top/bottom)",
	"vsplit":            "split the focused pane vertically (left/right)",
	"new-window":        "create a new window [name]",
	"rename-window":     "rename the active window <name>",
	"next-window":       "switch to the next window",
	"prev-window":       "switch to the previous window",
	"list-windows":      "list windows and their pane counts",
	"list-panes":        "list panes with ids, cwd and pids",
	"select-pane":       "focus a pane by <id|index>",
	"rename-pane":       "rename a pane <id> <name>",
	"close-pane":        "close a pane <id|index>",
	"send":              "send literal text to a pane <id|index> <text>",
	"run":               "run a shell command in a pane <id|index> <command>",
	"resize":            "resize a pane <id|index> <h|v> <+/-delta>",
	"env":               "show the detected agent environment (JSON)",
	"project":           "show the detected project",
	"git":               "show git status for the focused pane",
	"agent-context":     "render the assembled agent context",
	"agent-start":       "start a named agent [dir]",
	"agent-list":        "list running agents",
	"agent-switch":      "make a named agent active",
	"agent-status":      "show an agent's status",
	"agent-run":         "run a command with a named agent <name> <cmd>",
	"agent-stop":        "stop a named agent",
	"choose-pane":       "open the pane selection UI",
	"find":              "search pane contents",
	"synchronize-panes": "toggle synchronized input for the window",
	"cypher":            "open the Cypher prompt, or run a query [query]",
}

// openCommandPrompt arms the command prompt with an empty buffer.
func (a *App) openCommandPrompt() {
	a.cmdMode = true
	a.cmdBuf = ""
	a.cmdHistIdx = len(a.cmdHistory)
	a.resetCommandCompletion()
	a.errorMsg = ""
}

// resetCommandCompletion clears any in-progress Tab completion. It is called
// whenever the buffer changes so a stale candidate list is never shown.
func (a *App) resetCommandCompletion() {
	a.cmdCompActive = false
	a.cmdCompHead = ""
	a.cmdCompItems = nil
	a.cmdCompIdx = 0
}

// handleCommandInput processes keys while the command prompt is open.
func (a *App) handleCommandInput(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEsc, tcell.KeyCtrlC:
		a.cmdMode = false
		a.cmdBuf = ""
		a.resetCommandCompletion()
	case tcell.KeyEnter:
		input := strings.TrimSpace(a.cmdBuf)
		a.cmdMode = false
		a.cmdBuf = ""
		a.resetCommandCompletion()
		if input != "" {
			a.pushCommandHistory(input)
			a.executeCommand(input)
		}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if r := []rune(a.cmdBuf); len(r) > 0 {
			a.cmdBuf = string(r[:len(r)-1])
		}
		a.resetCommandCompletion()
	case tcell.KeyCtrlU:
		a.cmdBuf = ""
		a.resetCommandCompletion()
	case tcell.KeyUp:
		a.historyPrev()
		a.resetCommandCompletion()
	case tcell.KeyDown:
		a.historyNext()
		a.resetCommandCompletion()
	case tcell.KeyTab:
		a.completeCommand(1)
	case tcell.KeyBacktab:
		a.completeCommand(-1)
	case tcell.KeyRune:
		a.cmdBuf += string(ev.Rune())
		a.resetCommandCompletion()
	}
	return true
}

// pushCommandHistory records a command, avoiding immediate duplicates.
func (a *App) pushCommandHistory(cmd string) {
	if n := len(a.cmdHistory); n > 0 && a.cmdHistory[n-1] == cmd {
		return
	}
	a.cmdHistory = append(a.cmdHistory, cmd)
	if len(a.cmdHistory) > 200 {
		a.cmdHistory = a.cmdHistory[len(a.cmdHistory)-200:]
	}
	a.cmdHistIdx = len(a.cmdHistory)
}

// historyPrev moves to an older history entry.
func (a *App) historyPrev() {
	if len(a.cmdHistory) == 0 || a.cmdHistIdx <= 0 {
		return
	}
	a.cmdHistIdx--
	a.cmdBuf = a.cmdHistory[a.cmdHistIdx]
}

// historyNext moves to a newer history entry.
func (a *App) historyNext() {
	if a.cmdHistIdx >= len(a.cmdHistory)-1 {
		a.cmdHistIdx = len(a.cmdHistory)
		a.cmdBuf = ""
		return
	}
	a.cmdHistIdx++
	a.cmdBuf = a.cmdHistory[a.cmdHistIdx]
}

// paneArgCommands are the commands whose first argument names a pane, so Tab
// offers pane ids while completing that argument.
var paneArgCommands = map[string]bool{
	"select-pane": true, "rename-pane": true, "close-pane": true,
	"send": true, "run": true, "resize": true,
}

// completeCommand advances Tab completion by dir (+1 forward, -1 backward). The
// first press captures the candidate list and inserts the first match; later
// presses cycle through it without recomputing, so repeated Tab walks the
// options. Backtab cycles the other way.
func (a *App) completeCommand(dir int) {
	if !a.cmdCompActive {
		items := a.commandCompletions()
		if len(items) == 0 {
			return
		}
		prefix := commandWordPrefix(a.cmdBuf)
		a.cmdCompHead = a.cmdBuf[:len(a.cmdBuf)-len(prefix)]
		a.cmdCompItems = items
		a.cmdCompIdx = 0
		if dir < 0 {
			a.cmdCompIdx = len(items) - 1
		}
		a.cmdCompActive = true
	} else {
		n := len(a.cmdCompItems)
		if n == 0 {
			return
		}
		a.cmdCompIdx = ((a.cmdCompIdx+dir)%n + n) % n
	}
	a.cmdBuf = a.cmdCompHead + a.cmdCompItems[a.cmdCompIdx]
}

// commandCompletions returns the candidate tokens for the current buffer: the
// command names and aliases for the first token, or pane ids when completing a
// pane-taking command's first argument.
func (a *App) commandCompletions() []string {
	fields := strings.Fields(a.cmdBuf)
	trailingSpace := strings.HasSuffix(a.cmdBuf, " ")
	prefix := ""
	if !trailingSpace {
		prefix = commandWordPrefix(a.cmdBuf)
	}

	// Token position of the word being completed (0 = command name).
	argPos := len(fields)
	if !trailingSpace {
		argPos = len(fields) - 1
	}

	if argPos <= 0 {
		pool := make([]string, 0, len(commandNames)+len(commandAliases))
		pool = append(pool, commandNames...)
		for alias := range commandAliases {
			pool = append(pool, alias)
		}
		return matchingCI(pool, prefix)
	}
	if argPos == 1 && len(fields) > 0 {
		canon := fields[0]
		if c, ok := commandAliases[canon]; ok {
			canon = c
		}
		if paneArgCommands[canon] {
			var ids []string
			for _, info := range a.wm.AllPaneInfos() {
				ids = append(ids, info.ID)
			}
			return matchingCI(ids, prefix)
		}
	}
	return nil
}

// visibleCommandCompletions returns the rows to render in the dropdown and the
// selected index (-1 when completion is not active).
func (a *App) visibleCommandCompletions() ([]string, int) {
	if a.cmdCompActive {
		return a.cmdCompItems, a.cmdCompIdx
	}
	return a.commandCompletions(), -1
}

// commandCandidateLabel renders one completion row, annotating commands with
// their help line and aliases with the command they expand to.
func commandCandidateLabel(name string) string {
	if h, ok := commandHelp[name]; ok {
		return name + "  —  " + h
	}
	if canon, ok := commandAliases[name]; ok {
		return name + "  —  alias for " + canon
	}
	return name
}

// commandWordPrefix returns the trailing word under the cursor, consisting of
// the characters that may appear in a command name or pane id.
func commandWordPrefix(buf string) string {
	i := len(buf)
	for i > 0 && isCommandWordByte(buf[i-1]) {
		i--
	}
	return buf[i:]
}

// isCommandWordByte reports whether c may appear in a command name or pane id.
func isCommandWordByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '-' || c == '_':
		return true
	}
	return false
}

// matchingCI returns the unique entries beginning with prefix, case-
// insensitively, sorted.
func matchingCI(list []string, prefix string) []string {
	lp := strings.ToLower(prefix)
	var out []string
	seen := map[string]bool{}
	for _, s := range list {
		if seen[s] || !strings.HasPrefix(strings.ToLower(s), lp) {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// executeCommand parses and runs a command line, logging the outcome. A command
// is considered failed when it sets the error message (which is how every
// command reports a problem); otherwise it completed cleanly.
func (a *App) executeCommand(input string) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return
	}
	raw := fields[0]
	name := raw
	if canon, ok := commandAliases[name]; ok {
		name = canon
	}
	args := fields[1:]

	// Clear any stale error so the post-run check reflects only this command.
	a.errorMsg = ""
	a.dispatchCommand(name, args)

	if msg := a.errorMsg; msg != "" {
		slog.Error("command failed", "input", input, "command", name, "args", args, "err", msg)
		return
	}
	slog.Info("command executed", "input", input, "command", name, "args", args)
}

// dispatchCommand runs the canonical command name against the multiplexer.
func (a *App) dispatchCommand(name string, args []string) {
	switch name {
	case "help":
		a.showCommandHelp()
	case "quit":
		a.shouldQuit = true
	case "split":
		if err := a.wm.NewPane(); err != nil {
			a.SetErrorMsg(err.Error())
		} else {
			a.publishPaneCreated()
		}
	case "vsplit":
		if err := a.wm.NewPaneVertical(); err != nil {
			a.SetErrorMsg(err.Error())
		} else {
			a.publishPaneCreated()
		}
	case "new-window":
		name := strings.Join(args, " ")
		if err := a.wm.NewWindow(); err != nil {
			a.SetErrorMsg(err.Error())
		} else {
			if name != "" {
				a.wm.RenameWindow(name)
			}
			a.publishPaneCreated()
		}
	case "rename-window":
		if len(args) == 0 {
			a.SetErrorMsg("usage: rename-window <name>")
			return
		}
		a.wm.RenameWindow(strings.Join(args, " "))
	case "next-window":
		a.wm.NextWindow()
	case "prev-window":
		a.wm.PrevWindow()
	case "list-windows":
		a.showListWindows()
	case "list-panes":
		a.showListPanes()
	case "select-pane":
		a.commandSelectPane(args)
	case "rename-pane":
		a.commandRenamePane(args)
	case "close-pane":
		a.commandClosePane(args)
	case "send":
		a.commandSend(args, false)
	case "run":
		a.commandSend(args, true)
	case "resize":
		a.commandResize(args)
	case "env":
		a.setOverlay("Agent environment", a.harness.EnvironmentJSON())
	case "project":
		a.setOverlay("Agent project", a.harness.ProjectSummary())
	case "git":
		a.showGitStatus()
	case "agent-context":
		a.setOverlay("Agent context", a.agentContext())
	case "agent-start":
		a.commandAgentStart(args)
	case "agent-list":
		a.showListAgents()
	case "agent-switch":
		a.commandAgentSwitch(args)
	case "agent-status":
		a.commandAgentStatus(args)
	case "agent-run":
		a.commandAgentRun(args)
	case "agent-stop":
		a.commandAgentStop(args)
	case "choose-pane":
		a.openPanePicker()
	case "find":
		a.openPaneSearch()
	case "synchronize-panes":
		a.toggleSync()
	case "cypher":
		a.commandCypher(args)
	default:
		a.SetErrorMsg(fmt.Sprintf("Unknown command: %s (try :help)", name))
	}
}

// resolvePaneArg turns an id or 1-based index into a stable pane id.
func (a *App) resolvePaneArg(arg string) (string, bool) {
	if arg == "" {
		a.SetErrorMsg("missing pane id")
		return "", false
	}
	if info := a.wm.FindPane(arg); info != nil {
		return arg, true
	}
	if idx, err := strconv.Atoi(arg); err == nil {
		if p := a.wm.PaneByIndex(idx); p != nil {
			return p.ID(), true
		}
	}
	a.SetErrorMsg(fmt.Sprintf("no such pane: %s", arg))
	return "", false
}

// commandCypher opens the Cypher prompt with an empty buffer, or runs a
// one-off query when arguments were given (e.g. ":cypher :files" or
// ":cypher MATCH (n) RETURN n"). Run on the main goroutine.
func (a *App) commandCypher(args []string) {
	if len(args) == 0 {
		a.openGraphPrompt()
		return
	}
	a.runGraphQuery(strings.Join(args, " "))
}

func (a *App) commandSelectPane(args []string) {
	if len(args) == 0 {
		a.SetErrorMsg("usage: select-pane <id|index>")
		return
	}
	if id, ok := a.resolvePaneArg(args[0]); ok {
		if err := a.controller.SelectPane(id); err != nil {
			a.SetErrorMsg(err.Error())
		}
	}
}

func (a *App) commandRenamePane(args []string) {
	if len(args) < 2 {
		a.SetErrorMsg("usage: rename-pane <id|index> <name>")
		return
	}
	if id, ok := a.resolvePaneArg(args[0]); ok {
		if err := a.wm.RenamePaneByID(id, strings.Join(args[1:], " ")); err != nil {
			a.SetErrorMsg(err.Error())
		}
	}
}

func (a *App) commandClosePane(args []string) {
	if len(args) == 0 {
		a.SetErrorMsg("usage: close-pane <id|index>")
		return
	}
	id, ok := a.resolvePaneArg(args[0])
	if !ok {
		return
	}
	if quit, err := a.controller.ClosePane(id); err != nil {
		a.SetErrorMsg(err.Error())
	} else if quit {
		a.shouldQuit = true
	} else {
		a.events.Publish(agent.Event{Kind: agent.EventPaneClosed, PaneID: id})
	}
}

func (a *App) commandSend(args []string, enter bool) {
	if len(args) < 2 {
		a.SetErrorMsg("usage: send <id|index> <text>")
		return
	}
	id, ok := a.resolvePaneArg(args[0])
	if !ok {
		return
	}
	text := strings.Join(args[1:], " ")
	if err := a.controller.SendInput(id, []byte(text+"\r")); err != nil {
		a.SetErrorMsg(err.Error())
	}
}

func (a *App) commandResize(args []string) {
	if len(args) < 3 {
		a.SetErrorMsg("usage: resize <id|index> <h|v> <+/-delta>")
		return
	}
	id, ok := a.resolvePaneArg(args[0])
	if !ok {
		return
	}
	vertical := args[1] == "v"
	delta, err := strconv.Atoi(args[2])
	if err != nil {
		a.SetErrorMsg("delta must be an integer")
		return
	}
	if err := a.controller.ResizePane(id, vertical, delta); err != nil {
		a.SetErrorMsg(err.Error())
	}
}

// publishPaneCreated emits a pane-created event for the focused pane.
func (a *App) publishPaneCreated() {
	if p := a.wm.FocusedPane(); p != nil && a.events != nil {
		a.events.Publish(agent.Event{Kind: agent.EventPaneCreated, PaneID: p.ID()})
	}
}

// agentContext assembles the context the built-in agent would send to a model:
// base environment/project plus live pane and git state.
func (a *App) agentContext() string {
	c := a.harness.BaseContext()
	if p := a.wm.FocusedPane(); p != nil {
		info := fmt.Sprintf("id: %s\ncwd: %s\npid: %d\ntitle: %s\n",
			p.ID(), p.Cwd(), p.Pid(), p.Title())
		c.Add("Focused pane", info, 1)
		c.Add("Terminal output", agent.FilterOutput(p.Text(), 120), 2)
	}
	var layout strings.Builder
	for _, w := range a.wm.WindowInfos() {
		fmt.Fprintf(&layout, "window %d %q (%d panes, active=%t)\n", w.Index, w.Name, w.Panes, w.Active)
	}
	c.Add("Layout", layout.String(), 3)
	if g := a.harness.GitContext(context.Background()); g != "" {
		c.Add("Git", g, 2)
	}
	if hist := a.harness.RecentCommands(20); hist != "" {
		c.Add("Recent agent commands", hist, 3)
	}
	if fail := a.harness.LastFailure(); fail != "" {
		c.Add("Current errors", fail, 1)
	}
	return c.Render()
}

// showListWindows renders the window list.
func (a *App) showListWindows() {
	var b strings.Builder
	for _, w := range a.wm.WindowInfos() {
		marker := " "
		if w.Active {
			marker = "*"
		}
		fmt.Fprintf(&b, "%s %d: %s (%d panes)\n", marker, w.Index, w.Name, w.Panes)
	}
	a.setOverlay("Windows", b.String())
}

// showListPanes renders the pane list.
func (a *App) showListPanes() {
	var b strings.Builder
	for _, p := range a.wm.AllPaneInfos() {
		marker := " "
		if p.Focused {
			marker = "*"
		}
		fmt.Fprintf(&b, "%s [%s] win %d.%s cwd=%s pid=%d%s\n",
			marker, p.ID, p.WindowIndex, p.Window, p.Cwd, p.Pid, deadSuffix(p.Dead))
	}
	a.setOverlay("Panes", b.String())
}

// commandAgentStart starts a named agent, optionally rooted at a directory.
func (a *App) commandAgentStart(args []string) {
	if len(args) == 0 {
		a.SetErrorMsg("usage: agent-start <name> [dir]")
		return
	}
	dir, err := os.Getwd()
	if err != nil {
		dir = "/"
	}
	if len(args) > 1 {
		dir = args[1]
	}
	paneID := ""
	if p := a.wm.FocusedPane(); p != nil {
		paneID = p.ID()
	}
	if _, err := a.registry.Start(args[0], dir, agent.Policy{}, paneID); err != nil {
		a.SetErrorMsg(err.Error())
		return
	}
	a.setOverlay("Agents", a.agentsText())
}

// commandAgentSwitch makes a named agent active and points the harness at it.
func (a *App) commandAgentSwitch(args []string) {
	if len(args) == 0 {
		a.SetErrorMsg("usage: agent-switch <name>")
		return
	}
	if err := a.registry.Switch(args[0]); err != nil {
		a.SetErrorMsg(err.Error())
		return
	}
	a.harness = a.registry.Active().Harness
}

// commandAgentStop stops a named agent.
func (a *App) commandAgentStop(args []string) {
	if len(args) == 0 {
		a.SetErrorMsg("usage: agent-stop <name>")
		return
	}
	if err := a.registry.Stop(args[0]); err != nil {
		a.SetErrorMsg(err.Error())
		return
	}
	a.harness = a.registry.Active().Harness
}

// commandAgentStatus shows one agent's status, or all when unnamed.
func (a *App) commandAgentStatus(args []string) {
	if len(args) == 0 {
		a.setOverlay("Agent status", a.agentsText())
		return
	}
	ag := a.registry.Get(args[0])
	if ag == nil {
		a.SetErrorMsg("no such agent: " + args[0])
		return
	}
	cmds := 0
	for _, s := range a.registry.List() {
		if s.Name == ag.Name {
			cmds = s.Commands
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "name:      %s\nsession:   %s\npane:      %s\n",
		ag.Name, ag.SessionID, ag.PaneID)
	fmt.Fprintf(&b, "started:   %s\ncommands:  %d\n",
		ag.Started.Format(time.RFC3339), cmds)
	if info := ag.Harness.ProjectSummary(); info != "" {
		b.WriteString("\n")
		b.WriteString(info)
	}
	a.setOverlay("Agent "+ag.Name, b.String())
}

// commandAgentRun runs a shell command with a named agent's harness.
func (a *App) commandAgentRun(args []string) {
	if len(args) < 2 {
		a.SetErrorMsg("usage: agent-run <name> <command>")
		return
	}
	ag := a.registry.Get(args[0])
	if ag == nil {
		a.SetErrorMsg("no such agent: " + args[0])
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	res := ag.Harness.RunShell(ctx, strings.Join(args[1:], " "))
	a.registry.Touch(ag.Name)
	a.setOverlay("Agent "+ag.Name, agentResultText(res))
}

// agentResultText renders a command result for the overlay.
func agentResultText(res *agent.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "id:       %s\ncommand:  %s\nexit:     %d\nduration: %s\n",
		res.ID, res.Command, res.ExitCode, res.Duration.Round(time.Millisecond))
	if res.FailureSummary() != "" {
		b.WriteString("\n")
		b.WriteString(res.FailureSummary())
		b.WriteString("\n")
	}
	if out := res.Combined(); out != "" {
		b.WriteString("\n")
		b.WriteString(agent.FilterOutput(out, 200))
	}
	return b.String()
}

// showListAgents renders the agent list.
func (a *App) showListAgents() { a.setOverlay("Agents", a.agentsText()) }

// agentsText formats all agents as a status table.
func (a *App) agentsText() string {
	var b strings.Builder
	for _, s := range a.registry.List() {
		marker := " "
		if s.Active {
			marker = "*"
		}
		fmt.Fprintf(&b, "%s %-12s session=%s pane=%s cmds=%d idle=%s\n",
			marker, s.Name, s.SessionID, s.PaneID, s.Commands,
			time.Since(s.LastActivity).Round(time.Second))
	}
	if b.Len() == 0 {
		return "no agents running"
	}
	return b.String()
}

func deadSuffix(dead bool) string {
	if dead {
		return " (dead)"
	}
	return ""
}

// showCommandHelp renders the command reference.
func (a *App) showCommandHelp() {
	var b strings.Builder
	b.WriteString("Commands (aliases in parentheses):\n\n")
	for _, name := range commandNames {
		fmt.Fprintf(&b, "  %-16s %s\n", name, commandHelp[name])
	}
	b.WriteString("\nAliases: ")
	var aliases []string
	for al, canon := range commandAliases {
		aliases = append(aliases, al+"="+canon)
	}
	sort.Strings(aliases)
	b.WriteString(strings.Join(aliases, ", "))
	a.setOverlay("Help", b.String())
}

// renderCommandOverlay paints the command prompt as a bordered panel with a
// completion dropdown, mirroring the Cypher prompt. Called from the main loop.
func (a *App) renderCommandOverlay() {
	w, h := a.screen.Size()
	if h < 2 || w < 12 {
		return
	}

	items, selected := a.visibleCommandCompletions()
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
	drawString(a.screen, 2, top, w-3, " Command ", graphTitle)

	// Input line with the ':' prompt.
	inputRow := top + 1
	drawString(a.screen, 0, inputRow, w, " :", graphPromptSt)
	bufX := 2
	drawString(a.screen, bufX, inputRow, w-bufX, a.cmdBuf, graphInputSt)
	cursorX := bufX + len([]rune(a.cmdBuf))
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
		label := commandCandidateLabel(items[idx])
		text := "     " + label
		if idx == selected {
			st = graphSelected
			text = "   ❯ " + label
		}
		fillRow(a.screen, w, row, st)
		drawString(a.screen, 0, row, w, text, st)
	}

	// Hint row at the very bottom (omitted when the screen is too short).
	if hintRow := h - 1; hintRow > inputRow {
		hint := " Tab complete · ↑/↓ history · Enter run · Esc cancel"
		if len(items) > 0 {
			if selected >= 0 {
				hint = fmt.Sprintf(" %d/%d · Tab cycle · ↑/↓ history · Enter run · Esc cancel", selected+1, len(items))
			} else {
				hint = fmt.Sprintf(" %d matches · Tab complete · ↑/↓ history · Enter run · Esc cancel", len(items))
			}
		}
		drawString(a.screen, 0, hintRow, w, hint, graphHintSt)
	}
}
