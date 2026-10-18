package agent

import (
	"sync"
	"time"
)

// EventKind enumerates the internal events the harness and UI emit so an agent
// (or a plugin) can react to terminal and build activity without polling.
type EventKind string

const (
	EventPaneCreated     EventKind = "pane.created"
	EventPaneClosed      EventKind = "pane.closed"
	EventCommandStarted  EventKind = "command.started"
	EventCommandFinished EventKind = "command.finished"
	EventCommandFailed   EventKind = "command.failed"
	EventTerminalOutput  EventKind = "terminal.output"
	EventTerminalResize  EventKind = "terminal.resize"
	EventGitStateChanged EventKind = "git.changed"
	EventBuildFailed     EventKind = "build.failed"
	EventTestFailed      EventKind = "test.failed"
	EventTestPassed      EventKind = "test.passed"
)

// Event is a single observation with correlation fields.
type Event struct {
	Kind     EventKind         `json:"kind"`
	Time     time.Time         `json:"time"`
	PaneID   string            `json:"pane_id,omitempty"`
	WindowID string            `json:"window_id,omitempty"`
	Command  string            `json:"command,omitempty"`
	ExitCode int               `json:"exit_code,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
}

// EventBus is a small, non-blocking fan-out bus. Publish never blocks: a
// subscriber whose buffer is full drops the event (the same wake-up rule the
// pane output readers follow), so a slow agent can never stall the UI.
type EventBus struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewEventBus returns an empty bus.
func NewEventBus() *EventBus {
	return &EventBus{subs: map[chan Event]struct{}{}}
}

// Subscribe registers a new buffered subscriber.
func (b *EventBus) Subscribe(buffer int) <-chan Event {
	if buffer <= 0 {
		buffer = 64
	}
	ch := make(chan Event, buffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (b *EventBus) Unsubscribe(ch <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subs {
		if sub == ch {
			delete(b.subs, sub)
			close(sub)
			return
		}
	}
}

// Publish delivers e to every subscriber without blocking.
func (b *EventBus) Publish(e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subs {
		select {
		case sub <- e:
		default:
		}
	}
}
