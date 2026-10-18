// Live session state: the running multiplexer periodically mirrors its
// workspace to a small JSON file under the user's state directory. This lets a
// separate `tatami save` invocation snapshot a live instance (which cannot be
// introspected over a socket) and lets `tatami restore` seed a startup layout.
package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/atharva-0123-cs/tatami/layout"
)

// liveStateInterval throttles live-state writes from Render so a rapid stream
// of events does not hammer the disk.
const liveStateInterval = time.Second

// stateDir returns the per-user directory used for tatami state files,
// honouring XDG_STATE_HOME and falling back to ~/.local/state.
func stateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "tatami")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "tatami")
	}
	return filepath.Join(home, ".local", "state", "tatami")
}

// LiveStatePath is the file the running instance mirrors its workspace to.
func LiveStatePath() string { return filepath.Join(stateDir(), "live.json") }

// WriteLiveState atomically writes a workspace snapshot as JSON.
func WriteLiveState(spec *layout.SessionSpec) error {
	if spec == nil {
		return nil
	}
	dir := stateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "live.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, LiveStatePath())
}

// ReadLiveState loads the last workspace snapshot written by a running
// instance, or (nil, nil) when none exists.
func ReadLiveState() (*layout.SessionSpec, error) {
	data, err := os.ReadFile(LiveStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var spec layout.SessionSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// maybeWriteLiveState mirrors the workspace to disk at most once per
// liveStateInterval. Called from Render (main goroutine), so it may safely walk
// the layout tree.
func (a *App) maybeWriteLiveState() {
	if a.wm == nil {
		return
	}
	if !a.lastStateWrite.IsZero() && time.Since(a.lastStateWrite) < liveStateInterval {
		return
	}
	a.lastStateWrite = time.Now()
	_ = WriteLiveState(a.wm.Snapshot("live"))
}

// forceWriteLiveState mirrors the workspace to disk immediately.
func (a *App) forceWriteLiveState() {
	if a.wm == nil {
		return
	}
	a.lastStateWrite = time.Now()
	_ = WriteLiveState(a.wm.Snapshot("live"))
}
