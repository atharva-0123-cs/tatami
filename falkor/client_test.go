package falkor

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// newTestClient builds a Client pointed at addr. Protocol 2 and
// DisableIndentity keep the handshake to HELLO + the commands under test.
func newTestClient(addr string) *Client {
	return &Client{
		rdb:       redis.NewClient(&redis.Options{Addr: addr, Protocol: 2, DisableIndentity: true}),
		graphName: "test",
		addr:      addr,
	}
}

// closedAddr returns a 127.0.0.1 address that is guaranteed to refuse
// connections (a listener opened and immediately closed).
func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestConnectUnreachable(t *testing.T) {
	addr := closedAddr(t)
	c := newTestClient(addr)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err == nil {
		t.Fatal("Connect to a closed port: want error, got nil")
	}
	if c.Connected() {
		t.Fatal("client reported connected after a failed Connect")
	}
	connected, lastErr, gotAddr := c.Status()
	if connected || lastErr == nil || gotAddr != addr {
		t.Fatalf("Status = (%v, %v, %q), want (false, non-nil, %q)", connected, lastErr, gotAddr, addr)
	}
}

func TestHealthCheckUnreachable(t *testing.T) {
	c := newTestClient(closedAddr(t))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.HealthCheck(ctx); err == nil {
		t.Fatal("HealthCheck to a closed port: want error, got nil")
	}
	if c.Connected() {
		t.Fatal("client reported connected after a failed HealthCheck")
	}
}

func TestStartupRetriesThenFails(t *testing.T) {
	c := newTestClient(closedAddr(t))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Startup(ctx); err == nil {
		t.Fatal("Startup against a closed port: want error, got nil")
	}
	if c.Connected() {
		t.Fatal("client reported connected after a failed Startup")
	}
}

func TestConnectAndHealthCheckSuccess(t *testing.T) {
	addr, stop := startFakeRedis(t)
	defer stop()

	c := newTestClient(addr)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !c.Connected() {
		t.Fatal("client not connected after a successful Connect")
	}
	// A second Connect must be a no-op (already connected).
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	if err := c.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if got := c.GraphName(); got != "test" {
		t.Fatalf("GraphName = %q, want %q", got, "test")
	}
}

func TestIsConnError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"cypher syntax", errors.New("Cypher syntax error at line 1"), false},
		{"eof", io.EOF, true},
		{"net closed", net.ErrClosed, true},
		{"redis closed", redis.ErrClosed, true},
		{"refused", errors.New("dial tcp 127.0.0.1:1: connect: connection refused"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
	}
	for _, tc := range cases {
		if got := isConnError(tc.err); got != tc.want {
			t.Errorf("%s: isConnError = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// startFakeRedis starts a tiny RESP server that acknowledges HELLO errors and
// answers PING with +PONG, enough for a successful go-redis handshake. It
// returns the listen address and a stop function.
func startFakeRedis(t *testing.T) (string, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go serveFakeRedis(conn)
		}
	}()
	return l.Addr().String(), func() {
		_ = l.Close()
		<-done
	}
}

func serveFakeRedis(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	for {
		args, err := readRESPCommand(r)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		switch strings.ToUpper(args[0]) {
		case "HELLO":
			// A redis error makes go-redis fall back to RESP2 and continue.
			_, _ = w.WriteString("-ERR unknown command 'HELLO'\r\n")
		case "PING":
			_, _ = w.WriteString("+PONG\r\n")
		default:
			_, _ = w.WriteString("+OK\r\n")
		}
		if err := w.Flush(); err != nil {
			return
		}
	}
}

// readRESPCommand reads either an inline command or a RESP array of bulk
// strings and returns its arguments.
func readRESPCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "*") {
		if line == "" {
			return nil, nil
		}
		return strings.Fields(line), nil
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		hdr = strings.TrimRight(hdr, "\r\n")
		if !strings.HasPrefix(hdr, "$") {
			return nil, errors.New("unexpected RESP element")
		}
		l, err := strconv.Atoi(hdr[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, l+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:l]))
	}
	return args, nil
}
