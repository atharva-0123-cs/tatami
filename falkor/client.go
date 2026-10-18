package falkor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/atharva-0123-cs/tatami/config"
	"github.com/redis/go-redis/v9"
)

// connectTimeout bounds a single connectivity check (PING). Queries themselves
// are bounded by the caller's context.
const connectTimeout = 3 * time.Second

// Client wraps redis.Client to interact with FalkorDB using GRAPH.QUERY commands.
//
// Connectivity is tracked explicitly so the UI can surface a health indicator
// and so a dropped connection reconnects on the next query. The status fields
// are guarded by mu: health probes run on the UI goroutine while the background
// monitor updates the same state.
type Client struct {
	rdb       *redis.Client
	graphName string
	addr      string

	mu        sync.Mutex
	connected bool
	lastErr   error
	lastCheck time.Time
}

// GlobalClient provides access to the FalkorDB instance across Tatami.
var GlobalClient *Client

// Initialize sets up the global FalkorDB client based on the loaded config. It
// does not perform any network I/O; call Startup (or Connect/HealthCheck) to
// verify reachability.
func Initialize(cfg *config.Config) {
	addr := fmt.Sprintf("%s:%d", cfg.FalkorHost, cfg.FalkorPort)
	slog.Info("initializing falkordb client", "addr", addr, "graph", cfg.FalkorGraph, "auth", cfg.FalkorPassword != "")

	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.FalkorPassword,
		DB:       0,
	})

	GlobalClient = &Client{
		rdb:       rdb,
		graphName: cfg.FalkorGraph,
		addr:      addr,
	}
}

// Connect ensures the client can reach FalkorDB, issuing a PING when it is not
// already known-connected. It is safe for concurrent use; a successful connect
// is cached until a later transport error marks the client disconnected.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.connected {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	if err := c.rdb.Ping(ctx).Err(); err != nil {
		werr := fmt.Errorf("falkordb unreachable at %s (is FalkorDB/Redis running?): %w", c.addr, err)
		c.setStatus(false, werr)
		return werr
	}
	c.setStatus(true, nil)
	slog.Info("falkordb connected", "addr", c.addr, "graph", c.graphName)
	return nil
}

// HealthCheck probes FalkorDB with a bounded PING. Unlike Connect it always
// pings, so it detects a server that went away. A nil error means healthy.
func (c *Client) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	wasConnected := c.Connected()
	if err := c.rdb.Ping(ctx).Err(); err != nil {
		werr := fmt.Errorf("falkordb health check failed at %s: %w", c.addr, err)
		c.setStatus(false, werr)
		if wasConnected {
			slog.Warn("falkordb connection lost", "addr", c.addr, "err", err)
		}
		return werr
	}
	c.setStatus(true, nil)
	if !wasConnected {
		slog.Info("falkordb connected", "addr", c.addr, "graph", c.graphName)
	}
	return nil
}

// Startup runs the boot-time health check, retrying a few times with a short
// backoff to ride out a container that is still coming up. It never fails
// fatally: the caller logs the returned error and keeps running, since queries
// reconnect on demand.
func (c *Client) Startup(ctx context.Context) error {
	const attempts = 3
	var err error
	for i := 0; i < attempts; i++ {
		if err = c.HealthCheck(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(i+1) * 250 * time.Millisecond):
		}
	}
	return err
}

// Query executes a Cypher query against FalkorDB via GRAPH.QUERY. It connects
// on demand and, when a failure looks like a transport error, marks the client
// disconnected so the next call reconnects.
func (c *Client) Query(ctx context.Context, cypher string) (interface{}, error) {
	if err := c.Connect(ctx); err != nil {
		return nil, err
	}

	cmd := c.rdb.Do(ctx, "GRAPH.QUERY", c.graphName, cypher)
	if err := cmd.Err(); err != nil {
		if isConnError(err) {
			c.setStatus(false, err)
		}
		return nil, fmt.Errorf("graph query failed: %w", err)
	}
	return cmd.Result()
}

// setStatus records connectivity state under the mutex.
func (c *Client) setStatus(connected bool, err error) {
	c.mu.Lock()
	c.connected = connected
	c.lastErr = err
	c.lastCheck = time.Now()
	c.mu.Unlock()
}

// Connected reports the last known connectivity state.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Status returns the last known connectivity state, the last error (nil when
// healthy), and the configured host:port.
func (c *Client) Status() (connected bool, lastErr error, addr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected, c.lastErr, c.addr
}

// Addr returns the configured host:port.
func (c *Client) Addr() string { return c.addr }

// GraphName returns the configured graph name.
func (c *Client) GraphName() string { return c.graphName }

// PublishEvent broadcasts a message on a coordination channel. Agents in
// different panes use this to announce findings or wake each other; the graph
// remains the durable shared memory, pub/sub is the low-latency nudge.
func (c *Client) PublishEvent(ctx context.Context, channel, payload string) error {
	if err := c.Connect(ctx); err != nil {
		return err
	}
	return c.rdb.Publish(ctx, channel, payload).Err()
}

// SubscribeEvents returns a channel of messages published to channel and a
// cancel function that stops the subscription. The channel is closed when the
// context is cancelled or the cancel function is called.
func (c *Client) SubscribeEvents(ctx context.Context, channel string) (<-chan string, func(), error) {
	if err := c.Connect(ctx); err != nil {
		return nil, nil, err
	}
	sub := c.rdb.Subscribe(ctx, channel)
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, nil, err
	}
	out := make(chan string, 16)
	subCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(out)
		defer sub.Close()
		ch := sub.Channel()
		for {
			select {
			case <-subCtx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				select {
				case out <- msg.Payload:
				case <-subCtx.Done():
					return
				}
			}
		}
	}()
	return out, cancel, nil
}

// Close marks the client disconnected and closes the underlying redis
// connection.
func (c *Client) Close() error {
	c.setStatus(false, nil)
	if c.rdb != nil {
		return c.rdb.Close()
	}
	return nil
}

// isConnError reports whether err is a transport-level failure (as opposed to a
// Cypher or command error), so callers can decide to reconnect.
func isConnError(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, redis.ErrClosed) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "client is closed")
}
