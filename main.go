// Command tatami is a terminal multiplexer.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/atharva-0123-cs/tatami/config"
	"github.com/atharva-0123-cs/tatami/falkor"
	"github.com/atharva-0123-cs/tatami/format"
	"github.com/atharva-0123-cs/tatami/internal/app"
)

func openLogFile() *os.File {
	f, err := os.OpenFile("tatami.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tatami: cannot open log file: %v\n", err)
		os.Exit(1)
	}
	return f
}

// ("text" renders "[INFO] time file:line : message", "json" is for tooling)
func setupLogger(f *os.File, level slog.Level, kind string) {
	opts := &slog.HandlerOptions{Level: level, AddSource: true}
	var h slog.Handler
	if kind == "text" {
		h = format.NewConsoleHandler(f, opts)
	} else {
		h = slog.NewJSONHandler(f, opts)
	}
	slog.SetDefault(slog.New(h))
}

func main() {
	f := openLogFile()
	defer f.Close()

	// Initial logger with defaults so any config warnings are captured in the
	// log file; recreated below once the effective profile is known.
	setupLogger(f, slog.LevelInfo, "text")

	cfg := config.Load()
	setupLogger(f, cfg.LogLevel, cfg.LogFormat)

	falkor.Initialize(cfg)

	// Startup health check: probe FalkorDB once (with a couple of retries) and
	// report the result. A missing graph database is never fatal — graph
	// features simply reconnect on demand.
	healthCtx, cancelHealth := context.WithTimeout(context.Background(), 10*time.Second)
	if err := falkor.GlobalClient.Startup(healthCtx); err != nil {
		slog.Warn("falkordb unavailable at startup; graph features will retry on demand",
			"addr", falkor.GlobalClient.Addr(), "err", err)
	} else {
		slog.Info("falkordb ready at startup", "graph", falkor.GlobalClient.GraphName())
	}
	cancelHealth()

	defer falkor.GlobalClient.Close()

	slog.Info("tatami starting",
		"prefix", cfg.PrefixLabel,
		"mouse", cfg.Mouse,
		"pane_gap", cfg.PaneGap,
	)

	a, err := app.New(cfg)
	if err != nil {
		slog.Error(err.Error())
		fmt.Fprintf(os.Stderr, "tatami: %s\n", err)
		os.Exit(1)
	}
	defer a.Cleanup()

	a.Run()
}
