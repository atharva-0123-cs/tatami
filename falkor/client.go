package falkor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/atharva-0123-cs/tatami/config"
	"github.com/redis/go-redis/v9"
)

// Client wraps redis.Client to interact with FalkorDB using GRAPH.QUERY commands.
type Client struct {
	rdb       *redis.Client
	graphName string
	connected bool
}

// GlobalClient provides access to the FalkorDB instance across Tatami.
var GlobalClient *Client

// Initialize sets up the global FalkorDB client based on the loaded config.
func Initialize(cfg *config.Config) {
	addr := fmt.Sprintf("%s:%d", cfg.FalkorHost, cfg.FalkorPort)
	slog.Info("initializing falkordb client", "addr", addr, "graph", cfg.FalkorGraph)
	
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "",
		DB:       0,
	})
	
	GlobalClient = &Client{
		rdb:       rdb,
		graphName: cfg.FalkorGraph,
	}
}

// Connect pings Redis / FalkorDB to verify connectivity.
func (c *Client) Connect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := c.rdb.Ping(ctx).Err(); err != nil {
		c.connected = false
		return fmt.Errorf("falkordb ping failed (is FalkorDB/Redis running?): %w", err)
	}
	c.connected = true
	slog.Info("falkordb connected successfully", "graph", c.graphName)
	return nil
}

// Query executes a Cypher query against FalkorDB via GRAPH.QUERY.
func (c *Client) Query(ctx context.Context, cypher string) (interface{}, error) {
	if !c.connected {
		if err := c.Connect(ctx); err != nil {
			return nil, err
		}
	}
	// The GRAPH.QUERY command in FalkorDB (via Redis)
	cmd := c.rdb.Do(ctx, "GRAPH.QUERY", c.graphName, cypher)
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	return cmd.Result()
}

// Close closes the underlying redis connection.
func (c *Client) Close() error {
	if c.rdb != nil {
		return c.rdb.Close()
	}
	return nil
}
