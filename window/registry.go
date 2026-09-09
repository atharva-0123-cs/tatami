package window

// Registry tracks the ordered list of workspaces (windows) and which one is
// active.
//
// Each entry in Windows is a Window save slot holding that workspace's layout
// state (root/focused/paneCount/zoomed). The layout WindowManager keeps the
// active window's state live on itself and mirrors it into the save slot at
// CurrentWin whenever it switches windows.
type Registry struct {
	Windows    []*Window
	CurrentWin int    // index of the active window in Windows
	WindowName string // name of the currently active window
}

// NewRegistry creates a registry holding a single default window named "1".
func NewRegistry() *Registry {
	return &Registry{
		Windows:    []*Window{{Name: "1"}},
		CurrentWin: 0,
		WindowName: "1",
	}
}

// Count returns the total number of windows.
func (r *Registry) Count() int { return len(r.Windows) }

// Index returns the index of the active window.
func (r *Registry) Index() int { return r.CurrentWin }

// Name returns the name of the active window.
func (r *Registry) Name() string { return r.WindowName }

// Active returns the save slot of the active window.
func (r *Registry) Active() *Window { return r.Windows[r.CurrentWin] }

// Names returns the names of all windows in order.
func (r *Registry) Names() []string {
	names := make([]string, len(r.Windows))
	for i, w := range r.Windows {
		names[i] = w.Name
	}
	return names
}
