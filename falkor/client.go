package falkor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps redis.Client to interact with FalkorDB using GRAPH.QUERY commands.
type Client struct {
	rdb       *redis.Client
	graphName string
	connected bool
}

// NewClient creates a new FalkorDB client.
func NewClient(host string, port int, graphName string) *Client {
	addr := fmt.Sprintf("%s:%d", host, port)
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "",
		DB:       0,
	})
	if graphName == "" {
		graphName = "tatami"
	}
	return &Client{
		rdb:       rdb,
		graphName: graphName,
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
	cmd := c.rdb.Do(ctx, "GRAPH.QUERY", c.graphName, cypher)
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	return cmd.Result()
}

// Close closes the underlying redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}
