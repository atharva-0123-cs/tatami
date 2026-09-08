// Package config loads roge's runtime configuration ("roge.conf") and applies
// environment overrides on top of it.
//
// The file format is one `key = value` pair per line; blank lines and lines
// beginning with '#' are ignored. The file is looked up in order of priority:
//
//  1. the path in the ROGE_CONFIG environment variable, if set,
//  2. ./roge.conf in the working directory,
//  3. $XDG_CONFIG_HOME/roge/roge.conf (or ~/.config/roge/roge.conf when
//     XDG_CONFIG_HOME is unset).
//
// For every field, the ROGE_<NAME> environment variable overrides the file
// value (e.g. ROGE_PREFIX, ROGE_MOUSE). Unknown keys or malformed values are
// logged and the default is kept, so a broken config never prevents roge from
// starting.
package config

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Config holds every user-tunable runtime option that affects the main loop.
type Config struct {
	PrefixKey   tcell.Key // multiplexer prefix key (default Ctrl+A)
	PrefixLabel string    // human-readable prefix name, e.g. "Ctrl+A"
	Mouse       bool      // enables click-focus, seam-drag resize and wheel scroll
	StatusFg    tcell.Color
	StatusBg    tcell.Color
}

// defaultConfig returns the built-in settings used when no config file or
// environment override changes them.
func defaultConfig() *Config {
	return &Config{
		PrefixKey:   tcell.KeyCtrlA,
		PrefixLabel: "Ctrl+A",
		Mouse:       false,
		StatusFg:    tcell.PaletteColor(252) | tcell.ColorValid,
		StatusBg:    tcell.PaletteColor(235) | tcell.ColorValid,
	}
}

// Load reads the configuration file (if any) and applies environment
// overrides, returning a fully populated Config.
func Load() *Config {
	cfg := defaultConfig()
	if path := findPath(); path != "" {
		cfg.applyFile(path)
	}
	cfg.applyEnv()
	return cfg
}

// findPath resolves the config file location, or "" when none exists.
func findPath() string {
	if p := os.Getenv("ROGE_CONFIG"); p != "" {
		return p
	}
	if _, err := os.Stat("roge.conf"); err == nil {
		return "roge.conf"
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	p := filepath.Join(dir, "roge", "roge.conf")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// applyFile parses a roge.conf file and applies every recognized key,
// keeping the default for unknown or malformed values.
func (c *Config) applyFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("config: cannot open %s: %v", path, err)
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			log.Printf("config: ignoring line without '=': %q", line)
			continue
		}
		c.set(strings.TrimSpace(line[:eq]), strings.TrimSpace(line[eq+1:]))
	}
	if err := sc.Err(); err != nil {
		log.Printf("config: reading %s: %v", path, err)
	}
}

// applyEnv applies the ROGE_<NAME> environment overrides on top of the file.
func (c *Config) applyEnv() {
	if v := os.Getenv("ROGE_PREFIX"); v != "" {
		if key, label, err := parsePrefix(v); err == nil {
			c.PrefixKey, c.PrefixLabel = key, label
		} else {
			log.Printf("config: ROGE_PREFIX: %v", err)
		}
	}
	if v := os.Getenv("ROGE_MOUSE"); v != "" {
		if b, err := parseBool(v); err == nil {
			c.Mouse = b
		} else {
			log.Printf("config: ROGE_MOUSE: %v", err)
		}
	}
}

// set applies a single parsed key=value pair, logging and skipping anything it
// does not understand.
func (c *Config) set(key, value string) {
	switch strings.ToLower(key) {
	case "prefix":
		if k, label, err := parsePrefix(value); err == nil {
			c.PrefixKey, c.PrefixLabel = k, label
		} else {
			log.Printf("config: prefix: %v", err)
		}
	case "mouse":
		if b, err := parseBool(value); err == nil {
			c.Mouse = b
		} else {
			log.Printf("config: mouse: %v", err)
		}
	case "status_bg":
		if col, err := parseColor(value); err == nil {
			c.StatusBg = col
		} else {
			log.Printf("config: status_bg: %v", err)
		}
	case "status_fg":
		if col, err := parseColor(value); err == nil {
			c.StatusFg = col
		} else {
			log.Printf("config: status_fg: %v", err)
		}
	default:
		log.Printf("config: ignoring unknown key %q", key)
	}
}

// parsePrefix parses a prefix key written as "ctrl-<letter>" (a dash or plus
// sign is accepted between the two words) and returns the matching tcell key
// plus its canonical display label.
func parsePrefix(s string) (tcell.Key, string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	sep := strings.IndexAny(s, "-+")
	if sep < 0 || s[:sep] != "ctrl" {
		return 0, "", fmt.Errorf("unsupported prefix %q (use ctrl-<letter>)", s)
	}
	rest := s[sep+1:]
	if len(rest) != 1 || rest[0] < 'a' || rest[0] > 'z' {
		return 0, "", fmt.Errorf("unsupported prefix %q (use ctrl-<letter>)", s)
	}
	key := tcell.KeyCtrlA + tcell.Key(rest[0]-'a')
	return key, "Ctrl-" + strings.ToUpper(rest), nil
}

// parseColor parses a color written either as a decimal palette index
// (0-255, e.g. "235") or as #RRGGBB truecolor hex. It returns the color with
// the valid bit set, ready for tcell styles.
func parseColor(s string) (tcell.Color, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		hex := strings.TrimPrefix(s, "#")
		if len(hex) != 6 {
			return 0, fmt.Errorf("invalid color %q (want #RRGGBB)", s)
		}
		part := func(b string) (uint64, error) {
			return strconv.ParseUint(b, 16, 8)
		}
		r, err := part(hex[0:2])
		if err != nil {
			return 0, fmt.Errorf("invalid color %q (bad red component)", s)
		}
		g, err := part(hex[2:4])
		if err != nil {
			return 0, fmt.Errorf("invalid color %q (bad green component)", s)
		}
		b, err := part(hex[4:6])
		if err != nil {
			return 0, fmt.Errorf("invalid color %q (bad blue component)", s)
		}
		return tcell.NewRGBColor(int32(r), int32(g), int32(b)), nil
	}
	idx, err := strconv.Atoi(s)
	if err != nil || idx < 0 || idx > 255 {
		return 0, fmt.Errorf("invalid color %q (use 0-255 or #RRGGBB)", s)
	}
	return tcell.PaletteColor(idx) | tcell.ColorValid, nil
}

// parseBool parses on/off, true/false and 1/0 tokens.
func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "1", "yes":
		return true, nil
	case "off", "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q (use on/off)", s)
	}
}
