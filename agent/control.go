package agent

import (
	"fmt"

	"github.com/atharva-0123-cs/tatami/layout"
	"github.com/atharva-0123-cs/tatami/window"
)

// ControlPolicy is the explicit permission set governing what an agent may do
// to panes and windows. The zero value is fully locked down; a harness opts in
// to exactly the capabilities it needs. AllowedPanes/AllowedWindows, when
// non-empty, additionally restrict which stable IDs an agent may target, which
// is the second guard (alongside ID-based lookup) against controlling the wrong
// pane.
type ControlPolicy struct {
	AllowCreatePane   bool
	AllowCreateWindow bool
	AllowClose        bool
	AllowSendInput    bool
	AllowReadOutput   bool
	AllowResize       bool
	AllowSelect       bool
	AllowedPanes      []string
	AllowedWindows    []string
}

// Controller gives an agent a permission-checked view of the layout. Every
// method re-resolves the pane by its stable ID, so a stale reference can never
// silently address a different pane.
type Controller struct {
	WM     *layout.WindowManager
	Policy ControlPolicy
}

// NewController wraps a window manager with a control policy.
func NewController(wm *layout.WindowManager, p ControlPolicy) *Controller {
	return &Controller{WM: wm, Policy: p}
}

// ListPanes returns metadata for every pane across all windows.
func (c *Controller) ListPanes() []layout.PaneInfo { return c.WM.AllPaneInfos() }

// ListWindows returns metadata for every window.
func (c *Controller) ListWindows() []layout.WindowInfo { return c.WM.WindowInfos() }

// checkPane verifies the pane exists and is permitted by the policy.
func (c *Controller) checkPane(id string) error {
	if id == "" {
		return fmt.Errorf("no pane id given")
	}
	if c.WM.FindPane(id) == nil {
		return fmt.Errorf("pane %q not found", id)
	}
	if len(c.Policy.AllowedPanes) > 0 && !contains(c.Policy.AllowedPanes, id) {
		return fmt.Errorf("pane %q is outside the agent's allowed panes", id)
	}
	return nil
}

// CreatePane creates a pane in the active window and returns its stable ID.
func (c *Controller) CreatePane(dir string, vertical bool) (string, error) {
	if !c.Policy.AllowCreatePane {
		return "", fmt.Errorf("creating panes is not permitted")
	}
	p, err := c.WM.CreatePaneIn(dir, vertical)
	if err != nil {
		return "", err
	}
	return p.ID(), nil
}

// CreateWindow creates a window and returns its name.
func (c *Controller) CreateWindow(name, dir string) (string, error) {
	if !c.Policy.AllowCreateWindow {
		return "", fmt.Errorf("creating windows is not permitted")
	}
	if _, err := c.WM.CreateWindowIn(name, dir); err != nil {
		return "", err
	}
	return c.WM.WindowName(), nil
}

// SelectPane focuses a pane by ID.
func (c *Controller) SelectPane(id string) error {
	if !c.Policy.AllowSelect {
		return fmt.Errorf("selecting panes is not permitted")
	}
	if err := c.checkPane(id); err != nil {
		return err
	}
	return c.WM.FocusPaneByID(id)
}

// ClosePane closes a pane by ID. It reports whether the app should quit.
func (c *Controller) ClosePane(id string) (bool, error) {
	if !c.Policy.AllowClose {
		return false, fmt.Errorf("closing panes is not permitted")
	}
	if err := c.checkPane(id); err != nil {
		return false, err
	}
	return c.WM.ClosePaneByID(id)
}

// SendInput writes raw bytes to a pane's PTY.
func (c *Controller) SendInput(id string, data []byte) error {
	if !c.Policy.AllowSendInput {
		return fmt.Errorf("sending input is not permitted")
	}
	if err := c.checkPane(id); err != nil {
		return err
	}
	return c.WM.SendInputToPane(id, data)
}

// RunCommand types a command line into a pane and presses Enter.
func (c *Controller) RunCommand(id, command string) error {
	return c.SendInput(id, []byte(command+"\r"))
}

// ReadOutput returns a pane's full screen text.
func (c *Controller) ReadOutput(id string) (string, error) {
	if !c.Policy.AllowReadOutput {
		return "", fmt.Errorf("reading pane output is not permitted")
	}
	if err := c.checkPane(id); err != nil {
		return "", err
	}
	return c.WM.PaneTextByID(id)
}

// ResizePane adjusts a pane's split proportion. vertical selects the horizontal
// axis (width); otherwise the height is adjusted.
func (c *Controller) ResizePane(id string, vertical bool, delta int) error {
	if !c.Policy.AllowResize {
		return fmt.Errorf("resizing panes is not permitted")
	}
	if err := c.checkPane(id); err != nil {
		return err
	}
	dir := window.SplitHorizontal
	if vertical {
		dir = window.SplitVertical
	}
	return c.WM.ResizePaneByID(id, dir, delta)
}
