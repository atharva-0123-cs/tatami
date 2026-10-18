// Pane and window control API: a stable, ID-based surface for the built-in
// agent harness (and any external agent) to inspect, create, select, drive,
// resize and close panes without reaching into private layout fields. Every
// lookup is by explicit stable ID so an agent can never act on a stale numeric
// index and hit the wrong pane.
package layout

import (
	"fmt"

	"github.com/atharva-0123-cs/tatami/pane"
	"github.com/atharva-0123-cs/tatami/window"
)

// PaneInfo is a read-only snapshot of one pane, suitable for exposing to an
// agent or rendering in a selection UI. ID is stable for the pane's lifetime.
type PaneInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	Pid         int    `json:"pid"`
	Index       int    `json:"index"` // 1-based within its window
	Window      string `json:"window"`
	WindowIndex int    `json:"window_index"`
	Focused     bool   `json:"focused"`
	Dead        bool   `json:"dead"`
}

// WindowInfo is a read-only snapshot of one window.
type WindowInfo struct {
	Name   string `json:"name"`
	Index  int    `json:"index"`
	Panes  int    `json:"panes"`
	Active bool   `json:"active"`
}

// PaneInfos returns metadata for every pane in the active window.
func (wm *WindowManager) PaneInfos() []PaneInfo {
	return wm.infosFor(wm.root, wm.WindowName(), wm.WindowIndex())
}

// AllPaneInfos returns metadata for every pane across all windows.
func (wm *WindowManager) AllPaneInfos() []PaneInfo {
	var out []PaneInfo
	for i, w := range wm.Windows {
		root := w.Root
		if i == wm.CurrentWin {
			root = wm.root
		}
		out = append(out, wm.infosFor(root, w.Name, i+1)...)
	}
	return out
}

// infosFor walks one window's tree collecting pane metadata.
func (wm *WindowManager) infosFor(root *window.Node, winName string, winIdx int) []PaneInfo {
	var out []PaneInfo
	idx := 0
	wm.walk(root, func(n *window.Node) {
		if n.Leaf && n.Pane != nil {
			idx++
			p := n.Pane
			out = append(out, PaneInfo{
				ID: p.ID(), Name: p.Name(), Title: p.Title(), Cwd: p.Cwd(), Pid: p.Pid(),
				Index: idx, Window: winName, WindowIndex: winIdx,
				Focused: p == wm.focused, Dead: p.IsDead(),
			})
		}
	})
	return out
}

// WindowInfos returns metadata for every window.
func (wm *WindowManager) WindowInfos() []WindowInfo {
	out := make([]WindowInfo, 0, len(wm.Windows))
	for i, w := range wm.Windows {
		root := w.Root
		if i == wm.CurrentWin {
			root = wm.root
		}
		count := 0
		wm.walk(root, func(n *window.Node) {
			if n.Leaf && n.Pane != nil {
				count++
			}
		})
		out = append(out, WindowInfo{Name: w.Name, Index: i + 1, Panes: count, Active: i == wm.CurrentWin})
	}
	return out
}

// FindPane returns the pane with the given stable ID, or nil.
func (wm *WindowManager) FindPane(id string) *pane.Pane {
	var found *pane.Pane
	for i, w := range wm.Windows {
		root := w.Root
		if i == wm.CurrentWin {
			root = wm.root
		}
		wm.walk(root, func(n *window.Node) {
			if n.Leaf && n.Pane != nil && n.Pane.ID() == id {
				found = n.Pane
			}
		})
	}
	return found
}

// PaneByIndex returns the 1-based pane in the active window, or nil.
func (wm *WindowManager) PaneByIndex(i int) *pane.Pane {
	panes := wm.collectPanes()
	if i < 1 || i > len(panes) {
		return nil
	}
	return panes[i-1]
}

// FocusPaneByID switches to the pane's window and focuses it.
func (wm *WindowManager) FocusPaneByID(id string) error {
	for i, w := range wm.Windows {
		root := w.Root
		if i == wm.CurrentWin {
			root = wm.root
		}
		var target *pane.Pane
		wm.walk(root, func(n *window.Node) {
			if n.Leaf && n.Pane != nil && n.Pane.ID() == id {
				target = n.Pane
			}
		})
		if target != nil {
			if i != wm.CurrentWin {
				wm.switchTo(i)
			}
			wm.setFocus(target)
			return nil
		}
	}
	return fmt.Errorf("pane %q not found", id)
}

// CreatePaneIn creates a pane in the active window, starting its shell in dir,
// splitting the focused pane in the requested direction. It returns the new
// pane so the caller can read its ID.
func (wm *WindowManager) CreatePaneIn(dir string, vertical bool) (*pane.Pane, error) {
	wm.paneCount++
	p, err := pane.NewPaneInDir(fmt.Sprintf("%d", wm.paneCount), dir)
	if err != nil {
		return nil, err
	}
	if wm.root == nil {
		wm.root = &window.Node{Leaf: true, Pane: p}
		wm.focused = p
		return p, nil
	}
	splitDir := window.SplitHorizontal
	if vertical {
		splitDir = window.SplitVertical
	}
	wm.splitCurrent(p, splitDir)
	return p, nil
}

// CreateWindowIn creates a new window named name with a single pane rooted at
// dir, and switches to it.
func (wm *WindowManager) CreateWindowIn(name, dir string) (*pane.Pane, error) {
	if err := wm.NewWindow(); err != nil {
		return nil, err
	}
	if name != "" {
		wm.RenameWindow(name)
	}
	p := wm.FocusedPane()
	if p != nil && dir != "" {
		// NewWindow always starts a shell in the process cwd; replacing it with
		// one in dir would churn PTYs, so the directory is best-effort: the
		// caller drives the shell with `cd` if a specific dir is required.
		_ = dir
	}
	return p, nil
}

// ClosePaneByID closes the pane with the given stable ID. It returns true when
// the last pane was closed (the app should quit).
func (wm *WindowManager) ClosePaneByID(id string) (bool, error) {
	for i, w := range wm.Windows {
		root := w.Root
		if i == wm.CurrentWin {
			root = wm.root
		}
		var target *pane.Pane
		wm.walk(root, func(n *window.Node) {
			if n.Leaf && n.Pane != nil && n.Pane.ID() == id {
				target = n.Pane
			}
		})
		if target != nil {
			if i != wm.CurrentWin {
				wm.switchTo(i)
			}
			return wm.closePane(target), nil
		}
	}
	return false, fmt.Errorf("pane %q not found", id)
}

// ResizePaneByID focuses the pane and nudges its split proportion. dir selects
// the axis (SplitVertical for width, SplitHorizontal for height).
func (wm *WindowManager) ResizePaneByID(id string, dir window.SplitDir, delta int) error {
	if err := wm.FocusPaneByID(id); err != nil {
		return err
	}
	wm.adjustSize(dir, delta)
	return nil
}

// SendInputToPane writes raw bytes (keystrokes, a command line plus newline) to
// a pane's PTY.
func (wm *WindowManager) SendInputToPane(id string, data []byte) error {
	p := wm.FindPane(id)
	if p == nil {
		return fmt.Errorf("pane %q not found", id)
	}
	_, err := p.WriteInput(data)
	return err
}

// PaneTextByID returns a pane's full screen text (for an agent reading output).
func (wm *WindowManager) PaneTextByID(id string) (string, error) {
	p := wm.FindPane(id)
	if p == nil {
		return "", fmt.Errorf("pane %q not found", id)
	}
	return p.Text(), nil
}

// RenameWindow renames the active window.
func (wm *WindowManager) RenameWindow(name string) {
	if name == "" {
		return
	}
	wm.saveCurrent()
	wm.Active().Name = name
	wm.Registry.WindowName = name
}

// RenamePaneByID gives a pane a user-visible name.
func (wm *WindowManager) RenamePaneByID(id, name string) error {
	p := wm.FindPane(id)
	if p == nil {
		return fmt.Errorf("pane %q not found", id)
	}
	p.SetName(name)
	return nil
}

// FocusWindowByIndex switches to the 1-based window index.
func (wm *WindowManager) FocusWindowByIndex(i int) error {
	if i < 1 || i > len(wm.Windows) {
		return fmt.Errorf("window %d not found", i)
	}
	wm.switchTo(i - 1)
	return nil
}
