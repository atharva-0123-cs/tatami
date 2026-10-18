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

	// `tatami save [name]` and `tatami restore [name]` operate on FalkorDB
	// session snapshots without starting the interactive UI.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "save":
			runSave(cfg, argOr(os.Args, 2, "default"))
			return
		case "restore":
			runRestore(cfg, argOr(os.Args, 2, "default"))
			return
		case "help", "-h", "--help":
			fmt.Println("usage: tatami [save [name] | restore [name]]")
			return
		}
	}

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

// argOr returns args[i] when present and non-empty, else fallback.
func argOr(args []string, i int, fallback string) string {
	if i < len(args) && args[i] != "" {
		return args[i]
	}
	return fallback
}

// initGraph initializes the global FalkorDB client and probes reachability.
func initGraph(cfg *config.Config) error {
	falkor.Initialize(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return falkor.GlobalClient.Startup(ctx)
}

// runSave snapshots the live workspace (mirrored to disk by the running
// instance) into a named FalkorDB session.
func runSave(cfg *config.Config, name string) {
	if err := initGraph(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "tatami save: FalkorDB unavailable: %v\n", err)
		os.Exit(1)
	}
	defer falkor.GlobalClient.Close()

	spec, err := app.ReadLiveState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tatami save: cannot read live state: %v\n", err)
		os.Exit(1)
	}
	if spec == nil {
		fmt.Fprintln(os.Stderr, "tatami save: no running tatami session found (live state missing)")
		os.Exit(1)
	}
	spec.Name = name
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := falkor.SaveSession(ctx, falkor.GlobalClient, spec); err != nil {
		fmt.Fprintf(os.Stderr, "tatami save: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("saved session %q (%d window(s))\n", name, len(spec.Windows))
}

// runRestore rebuilds and runs the interactive UI from a saved session.
func runRestore(cfg *config.Config, name string) {
	if err := initGraph(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "tatami restore: FalkorDB unavailable: %v\n", err)
		os.Exit(1)
	}
	defer falkor.GlobalClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	spec, err := falkor.RestoreSession(ctx, falkor.GlobalClient, name)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tatami restore: %v\n", err)
		os.Exit(1)
	}

	a, err := app.NewFromSession(cfg, spec)
	if err != nil {
		slog.Error(err.Error())
		fmt.Fprintf(os.Stderr, "tatami: %s\n", err)
		os.Exit(1)
	}
	defer a.Cleanup()
	a.Run()
}
