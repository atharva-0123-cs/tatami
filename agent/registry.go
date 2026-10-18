package agent

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Agent is a named harness instance bound to a pane. Its SessionID is a stable
// identifier an external supervisor can log and correlate.
type Agent struct {
	Name       string    `json:"name"`
	SessionID  string    `json:"session_id"`
	PaneID     string    `json:"pane_id"`
	Started    time.Time `json:"started"`
	Harness    *Harness  `json:"-"`
	Policy     Policy    `json:"-"`
	lastActive time.Time
	commands   int
}

// Status is a snapshot of an agent for the UI/agent-status command.
type Status struct {
	Name         string    `json:"name"`
	SessionID    string    `json:"session_id"`
	PaneID       string    `json:"pane_id"`
	Started      time.Time `json:"started"`
	LastActivity time.Time `json:"last_activity"`
	Commands     int       `json:"commands"`
	Active       bool      `json:"active"`
}

// Registry tracks the agents running inside one tatami instance and which one
// is active. It is safe for concurrent use.
type Registry struct {
	mu     sync.Mutex
	agents map[string]*Agent
	order  []string
	active string
	seq    int
}

// NewRegistry returns an empty agent registry.
func NewRegistry() *Registry {
	return &Registry{agents: map[string]*Agent{}}
}

// Start registers a new named agent rooted at dir. It fails if the name is
// taken. An empty name is auto-generated.
func (r *Registry) Start(name, dir string, policy Policy, paneID string) (*Agent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" {
		r.seq++
		name = fmt.Sprintf("agent-%d", r.seq)
	}
	if _, ok := r.agents[name]; ok {
		return nil, fmt.Errorf("agent %q already exists", name)
	}
	r.seq++
	now := time.Now()
	a := &Agent{
		Name:       name,
		SessionID:  fmt.Sprintf("%s-%d", name, r.seq),
		PaneID:     paneID,
		Started:    now,
		Harness:    New(dir, policy),
		Policy:     policy,
		lastActive: now,
	}
	r.agents[name] = a
	r.order = append(r.order, name)
	if r.active == "" {
		r.active = name
	}
	return a, nil
}

// Get returns the named agent, or nil.
func (r *Registry) Get(name string) *Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agents[name]
}

// Active returns the active agent, or nil.
func (r *Registry) Active() *Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agents[r.active]
}

// ActiveName returns the name of the active agent.
func (r *Registry) ActiveName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// Switch makes name the active agent.
func (r *Registry) Switch(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.agents[name]; !ok {
		return fmt.Errorf("no such agent: %s", name)
	}
	r.active = name
	return nil
}

// Stop removes an agent. If it was active, the first remaining agent becomes
// active.
func (r *Registry) Stop(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.agents[name]; !ok {
		return fmt.Errorf("no such agent: %s", name)
	}
	delete(r.agents, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	if r.active == name {
		r.active = ""
		if len(r.order) > 0 {
			r.active = r.order[0]
		}
	}
	return nil
}

// Touch records activity for an agent (called after it runs a command).
func (r *Registry) Touch(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.agents[name]; a != nil {
		a.lastActive = time.Now()
		a.commands++
	}
}

// List returns a snapshot of all agents, sorted by name.
func (r *Registry) List() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.agents))
	for n := range r.agents {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Status, 0, len(names))
	for _, n := range names {
		a := r.agents[n]
		out = append(out, Status{
			Name: a.Name, SessionID: a.SessionID, PaneID: a.PaneID,
			Started: a.Started, LastActivity: a.lastActive,
			Commands: a.commands, Active: n == r.active,
		})
	}
	return out
}
