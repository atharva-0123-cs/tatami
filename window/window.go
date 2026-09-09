package window

import "github.com/atharva-0123-cs/tatami/pane"

// Window is the save slot for one workspace's layout state.
//
// Only the active window's state lives directly on the layout WindowManager;
// every window also has a matching Window here (the active one tracked by the
// Registry) that mirrors that state and is refreshed on every switch.
type Window struct {
	Name      string
	Root      *Node
	Focused   *pane.Pane
	PaneCount int
	Zoomed    *pane.Pane
}
