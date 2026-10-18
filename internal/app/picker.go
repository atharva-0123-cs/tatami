// Pane selection UI, pane content search, and pane input synchronization.
//
// The picker is a modal overlay (like the graph prompt) driven from the main
// event loop. prefix+P opens the pane list; prefix+f opens content search.
// prefix+S toggles synchronized input, which broadcasts every forwarded
// keystroke to all panes in the active window (tmux's synchronize-panes).
package app

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// pickKind selects which picker overlay (if any) is showing.
type pickKind int

const (
	pickNone pickKind = iota
	pickPanes
	pickSearch
)

// searchPhase distinguishes typing the query from browsing its results.
const (
	searchTyping = iota
	searchBrowsing
)

// pickItem is one row of a picker list.
type pickItem struct {
	Label  string
	PaneID string
}

// openPanePicker opens the modal pane selector listing every pane.
func (a *App) openPanePicker() {
	a.pickKind = pickPanes
	a.pickItems = nil
	for _, p := range a.wm.AllPaneInfos() {
		marker := " "
		if p.Focused {
			marker = "*"
		}
		label := fmt.Sprintf("%s %-4d win %d.%s  cwd=%s", marker, p.Index, p.WindowIndex, p.Window, p.Cwd)
		a.pickItems = append(a.pickItems, pickItem{Label: label, PaneID: p.ID})
	}
	a.pickIndex = 0
	a.errorMsg = ""
}

// openPaneSearch opens the modal content search over every pane's screen text.
func (a *App) openPaneSearch() {
	a.pickKind = pickSearch
	a.searchBuf = ""
	a.searchPhase = searchTyping
	a.pickItems = nil
	a.pickIndex = 0
	a.errorMsg = ""
}

// handlePickerInput processes keys while a picker overlay is open.
func (a *App) handlePickerInput(ev *tcell.EventKey) bool {
	if a.pickKind == pickSearch && a.searchPhase == searchTyping {
		switch ev.Key() {
		case tcell.KeyEsc, tcell.KeyCtrlC:
			a.closePicker()
		case tcell.KeyEnter:
			a.runPaneSearch()
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if r := []rune(a.searchBuf); len(r) > 0 {
				a.searchBuf = string(r[:len(r)-1])
			}
		case tcell.KeyCtrlU:
			a.searchBuf = ""
		case tcell.KeyRune:
			a.searchBuf += string(ev.Rune())
		}
		return true
	}

	// Browsing phase (pane list and search results share it).
	switch ev.Key() {
	case tcell.KeyEsc, tcell.KeyCtrlC:
		a.closePicker()
		return true
	case tcell.KeyUp:
		a.movePick(-1)
		return true
	case tcell.KeyDown:
		a.movePick(1)
		return true
	case tcell.KeyEnter:
		a.acceptPick()
		return true
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			a.closePicker()
		case 'j':
			a.movePick(1)
		case 'k':
			a.movePick(-1)
		}
	}
	return true
}

// closePicker dismisses the picker overlay.
func (a *App) closePicker() {
	a.pickKind = pickNone
	a.pickItems = nil
	a.pickIndex = 0
	a.searchPhase = searchTyping
}

// movePick moves the selection by delta, clamped to the list.
func (a *App) movePick(delta int) {
	if len(a.pickItems) == 0 {
		return
	}
	a.pickIndex += delta
	if a.pickIndex < 0 {
		a.pickIndex = 0
	}
	if a.pickIndex >= len(a.pickItems) {
		a.pickIndex = len(a.pickItems) - 1
	}
}

// acceptPick focuses the selected pane and closes the picker.
func (a *App) acceptPick() {
	if a.pickIndex < 0 || a.pickIndex >= len(a.pickItems) {
		a.closePicker()
		return
	}
	id := a.pickItems[a.pickIndex].PaneID
	a.closePicker()
	if err := a.wm.FocusPaneByID(id); err != nil {
		a.SetErrorMsg(err.Error())
	}
}

// runPaneSearch scans every pane's text for the query and builds the result
// list, one row per matching pane (showing the first matching line).
func (a *App) runPaneSearch() {
	q := strings.TrimSpace(a.searchBuf)
	a.pickItems = nil
	a.pickIndex = 0
	a.searchPhase = searchBrowsing
	if q == "" {
		return
	}
	needle := strings.ToLower(q)
	for _, info := range a.wm.AllPaneInfos() {
		text, err := a.wm.PaneTextByID(info.ID)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(text, "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				trimmed := strings.TrimSpace(line)
				if r := []rune(trimmed); len(r) > 80 {
					trimmed = string(r[:80])
				}
				label := fmt.Sprintf("[%s] %d: %s", info.ID, i+1, trimmed)
				a.pickItems = append(a.pickItems, pickItem{Label: label, PaneID: info.ID})
				break
			}
		}
	}
	if len(a.pickItems) == 0 {
		a.SetErrorMsg("no matches for " + q)
	}
}

// toggleSync flips synchronized-input for the active window.
func (a *App) toggleSync() {
	if a.syncWindows == nil {
		a.syncWindows = map[string]bool{}
	}
	name := a.wm.WindowName()
	a.syncWindows[name] = !a.syncWindows[name]
	if a.syncWindows[name] {
		a.SetErrorMsg("pane sync: on (" + name + ")")
	} else {
		a.SetErrorMsg("pane sync: off (" + name + ")")
	}
}

// syncOn reports whether synchronized input is active for the current window.
func (a *App) syncOn() bool {
	return a.syncWindows[a.wm.WindowName()]
}

// renderPickerOverlay paints the active picker overlay.
func (a *App) renderPickerOverlay() {
	w, h := a.screen.Size()
	if h < 5 || w < 20 {
		return
	}
	x0, y0, x1, y1 := 1, 1, w-2, h-2
	for y := y0; y <= y1; y++ {
		fillRow(a.screen, w, y, graphPanel)
	}

	title := "Panes"
	if a.pickKind == pickSearch {
		title = "Search: " + a.searchBuf
	}
	drawString(a.screen, x0+1, y0, x1-x0-1, " "+title+" ", graphTitle)

	bodyTop, bodyBottom := y0+2, y1-1
	bodyH := bodyBottom - bodyTop + 1
	if bodyH < 1 {
		return
	}

	// Keep the selected row visible.
	if a.pickIndex < a.pickTop {
		a.pickTop = a.pickIndex
	}
	if a.pickIndex >= a.pickTop+bodyH {
		a.pickTop = a.pickIndex - bodyH + 1
	}
	if a.pickTop < 0 {
		a.pickTop = 0
	}
	for i := a.pickTop; i < len(a.pickItems) && i-a.pickTop < bodyH; i++ {
		st := graphPanel
		if i == a.pickIndex {
			st = graphBar
		}
		drawString(a.screen, x0+1, bodyTop+(i-a.pickTop), x1-x0-1, a.pickItems[i].Label, st)
	}

	if a.pickKind == pickSearch && a.searchPhase == searchTyping {
		promptRow := y0 + 1
		fillRow(a.screen, w, promptRow, graphBar)
		text := "find> " + a.searchBuf
		drawString(a.screen, x0+1, promptRow, x1-x0-1, text, graphBar)
		a.screen.ShowCursor(min(len([]rune(text))+1, w-1), promptRow)
	} else {
		hint := "j/k move  Enter select  q/Esc close"
		if a.pickKind == pickSearch {
			hint = fmt.Sprintf("%d match(es)  j/k move  Enter jump  q/Esc close", len(a.pickItems))
		}
		drawString(a.screen, x0+1, y1, x1-x0-1, hint, graphTitle)
		a.screen.HideCursor()
	}
}
