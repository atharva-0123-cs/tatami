// Session serialization: converting the whole workspace (every window's layout
// tree, the working directory of each pane, focus and the active window) to and
// from a portable, JSON-encodable spec. Snapshot is used by the FalkorDB
// persistence layer and by `tatami save`/`tatami restore`; Restore rebuilds a
// live workspace, spawning a fresh shell in each pane's recorded directory.
package layout

import (
	"fmt"

	"github.com/atharva-0123-cs/tatami/pane"
	"github.com/atharva-0123-cs/tatami/window"
)

// NodeSpec is a serializable layout tree node. A leaf records the directory its
// shell should start in; a container records its split direction, child sizes
// and children.
type NodeSpec struct {
	Leaf     bool        `json:"leaf"`
	Dir      int         `json:"dir,omitempty"`
	Sizes    []int       `json:"sizes,omitempty"`
	Children []*NodeSpec `json:"children,omitempty"`
	Cwd      string      `json:"cwd,omitempty"`
	Pid      int         `json:"pid,omitempty"`
	Ppid     int         `json:"ppid,omitempty"`
}

// WindowSpec is a serializable workspace: its name, layout tree and which pane
// (in depth-first order) holds focus.
type WindowSpec struct {
	Name         string    `json:"name"`
	Root         *NodeSpec `json:"root"`
	FocusedIndex int       `json:"focused_index"`
}

// SessionSpec is a serializable snapshot of the entire multiplexer: every
// window plus the index of the active one.
type SessionSpec struct {
	Name    string       `json:"name"`
	Active  int          `json:"active"`
	Windows []WindowSpec `json:"windows"`
}

// Snapshot captures the live workspace. It first mirrors the active window's
// state into its save slot (so switching windows is not required) and then
// serializes every window's tree, recording each pane's working directory.
func (wm *WindowManager) Snapshot(name string) *SessionSpec {
	wm.saveCurrent()
	spec := &SessionSpec{Name: name, Active: wm.CurrentWin}
	for _, w := range wm.Windows {
		spec.Windows = append(spec.Windows, serializeWindow(w))
	}
	return spec
}

// serializeWindow converts one window save slot to a WindowSpec.
func serializeWindow(w *window.Window) WindowSpec {
	ws := WindowSpec{Name: w.Name, FocusedIndex: -1}
	counter := 0
	ws.Root, ws.FocusedIndex = serializeNode(w.Root, w.Focused, &counter)
	return ws
}

// serializeNode converts a tree node recursively. counter tracks the
// depth-first leaf index so the focused pane can be recorded by position.
func serializeNode(n *window.Node, focused *pane.Pane, counter *int) (*NodeSpec, int) {
	if n == nil {
		return nil, -1
	}
	if n.Leaf {
		spec := &NodeSpec{Leaf: true}
		focusIdx := -1
		if n.Pane != nil {
			spec.Cwd = n.Pane.Cwd()
			spec.Pid = n.Pane.Pid()
			spec.Ppid = n.Pane.ParentPid()
			if n.Pane == focused {
				focusIdx = *counter
			}
		}
		*counter++
		return spec, focusIdx
	}
	spec := &NodeSpec{
		Leaf:  false,
		Dir:   int(n.Dir),
		Sizes: append([]int(nil), n.Sizes...),
	}
	focusIdx := -1
	for _, c := range n.Children {
		child, idx := serializeNode(c, focused, counter)
		if child == nil {
			continue
		}
		spec.Children = append(spec.Children, child)
		if idx >= 0 {
			focusIdx = idx
		}
	}
	return spec, focusIdx
}

// Restore replaces the entire workspace with the given snapshot, closing every
// pane currently in the tree and spawning a fresh shell for each restored pane
// in its recorded directory. It returns an error only when no pane at all could
// be created.
func (wm *WindowManager) Restore(spec *SessionSpec) error {
	if spec == nil || len(spec.Windows) == 0 {
		return fmt.Errorf("empty session")
	}

	// Tear down the current panes before rebuilding so PTYs are not leaked.
	for _, p := range wm.CollectAllPanes() {
		_ = p.Close()
	}

	wm.root = nil
	wm.focused = nil
	wm.zoomed = nil
	wm.paneCount = 0
	wm.Windows = nil

	created := 0
	for _, ws := range spec.Windows {
		w, n, err := wm.restoreWindow(ws)
		if err != nil {
			return err
		}
		created += n
		wm.Windows = append(wm.Windows, w)
	}
	if created == 0 {
		wm.Windows = nil
		return fmt.Errorf("session contained no usable panes")
	}

	active := spec.Active
	if active < 0 || active >= len(wm.Windows) {
		active = 0
	}
	wm.CurrentWin = active
	w := wm.Windows[active]
	wm.root = w.Root
	wm.focused = w.Focused
	wm.paneCount = w.PaneCount
	wm.zoomed = w.Zoomed
	wm.Registry.WindowName = w.Name
	return nil
}

// restoreWindow builds one window's tree, returning the save slot and the
// number of panes created.
func (wm *WindowManager) restoreWindow(ws WindowSpec) (*window.Window, int, error) {
	counter := 0
	created := 0
	var focused *pane.Pane

	var build func(n *NodeSpec) (*window.Node, error)
	build = func(n *NodeSpec) (*window.Node, error) {
		if n == nil {
			return nil, nil
		}
		if n.Leaf {
			wm.paneCount++
			created++
			p, err := pane.NewPaneInDir(fmt.Sprintf("%d", wm.paneCount), n.Cwd)
			if err != nil {
				return nil, err
			}
			if counter == ws.FocusedIndex {
				focused = p
			}
			counter++
			return &window.Node{Leaf: true, Pane: p}, nil
		}
		node := &window.Node{Leaf: false, Dir: window.SplitDir(n.Dir)}
		for i, c := range n.Children {
			child, err := build(c)
			if err != nil {
				return nil, err
			}
			if child == nil {
				continue
			}
			size := 1
			if i < len(n.Sizes) && n.Sizes[i] > 0 {
				size = n.Sizes[i]
			}
			node.AddChild(child, size)
		}
		return node, nil
	}

	root, err := build(ws.Root)
	if err != nil {
		return nil, created, err
	}
	if focused == nil {
		// Fall back to the first leaf when the recorded focus is stale.
		wm.walk(root, func(n *window.Node) {
			if focused == nil && n.Leaf && n.Pane != nil {
				focused = n.Pane
			}
		})
	}
	name := ws.Name
	if name == "" {
		name = fmt.Sprintf("%d", len(wm.Windows)+1)
	}
	w := &window.Window{Name: name, Root: root, Focused: focused, PaneCount: wm.paneCount}
	return w, created, nil
}
