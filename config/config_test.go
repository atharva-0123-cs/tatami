package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestDefaults verifies the built-in configuration matches the historical
// hard-coded behavior (Ctrl+A prefix, mouse off, dark status colors).
func TestDefaults(t *testing.T) {
	cfg := defaultConfig()
	if cfg.PrefixKey != tcell.KeyCtrlA || cfg.PrefixLabel != "Ctrl+A" {
		t.Fatalf("prefix = %v/%q, want Ctrl+A", cfg.PrefixKey, cfg.PrefixLabel)
	}
	if cfg.Mouse {
		t.Fatal("mouse should default to off")
	}
	if cfg.StatusFg == 0 || cfg.StatusBg == 0 {
		t.Fatal("status colors should be set")
	}
}

// TestParseBooleans covers every accepted spellings of an on/off value.
func TestParseBooleans(t *testing.T) {
	for _, ok := range []string{"on", "true", "1", "yes", "ON", " True "} {
		if v, err := parseBool(ok); err != nil || !v {
			t.Fatalf("parseBool(%q) = %v, %v; want true", ok, v, err)
		}
	}
	for _, no := range []string{"off", "false", "0", "no"} {
		if v, err := parseBool(no); err != nil || v {
			t.Fatalf("parseBool(%q) = %v, %v; want false", no, v, err)
		}
	}
	if _, err := parseBool("maybe"); err == nil {
		t.Fatal("parseBool should reject garbage")
	}
}

// TestParsePrefix accepts ctrl-a/ctrl+a and yields the matching key and label,
// and rejects everything else.
func TestParsePrefix(t *testing.T) {
	cases := map[string]struct {
		key   tcell.Key
		label string
	}{
		"ctrl-a": {tcell.KeyCtrlA, "Ctrl-A"},
		"ctrl+b": {tcell.KeyCtrlB, "Ctrl-B"},
		"CTRL-Z": {tcell.KeyCtrlZ, "Ctrl-Z"},
	}
	for in, want := range cases {
		key, label, err := parsePrefix(in)
		if err != nil || key != want.key || label != want.label {
			t.Fatalf("parsePrefix(%q) = %v/%q, %v; want %v/%q", in, key, label, err, want.key, want.label)
		}
	}
	for _, bad := range []string{"a", "ctrl", "ctrl-", "meta-x", "ctrl-1", "ctrl-aa"} {
		if _, _, err := parsePrefix(bad); err == nil {
			t.Fatalf("parsePrefix(%q) should fail", bad)
		}
	}
}

// TestParseColor covers palette-index and #RRGGBB forms plus garbage.
func TestParseColor(t *testing.T) {
	idx, err := parseColor("235")
	if err != nil || idx != tcell.PaletteColor(235) {
		t.Fatalf("parseColor(235) = %v, %v; want palette 235", idx, err)
	}
	rgb, err := parseColor(" #ff00ff ")
	if err != nil || rgb != tcell.NewRGBColor(255, 0, 255) {
		t.Fatalf("parseColor(#ff00ff) = %v, %v; want truecolor", rgb, err)
	}
	for _, bad := range []string{"#ff", "#gg0000", "256", "-1", "red"} {
		if _, err := parseColor(bad); err == nil {
			t.Fatalf("parseColor(%q) should fail", bad)
		}
	}
}

// TestParsePaneGap accepts cell counts within range and rejects bad values.
func TestParsePaneGap(t *testing.T) {
	for _, ok := range []string{"0", "1", "8"} {
		if v, err := parsePaneGap(ok); err != nil || v < 0 || v > 8 {
			t.Fatalf("parsePaneGap(%q) = %d, %v; want in range", ok, v, err)
		}
	}
	for _, bad := range []string{"-1", "9", "one", ""} {
		if _, err := parsePaneGap(bad); err == nil {
			t.Fatalf("parsePaneGap(%q) should fail", bad)
		}
	}
}

// TestLoadFile exercises file resolution through TATAMI_CONFIG and key parsing,
// including comments, blank lines and unknown keys.
func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tatami.conf")
	content := "# tatami demo config\n\nprefix = ctrl-b\nmouse = on\nstatus_bg = #101418\nstatus_fg = 250\npane_gap = 2\ndefault = ignored\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TATAMI_CONFIG", path)
	t.Setenv("TATAMI_PREFIX", "")
	t.Setenv("TATAMI_MOUSE", "")
	t.Setenv("TATAMI_PANE_GAP", "")

	cfg := Load()
	if cfg.PrefixKey != tcell.KeyCtrlB || cfg.PrefixLabel != "Ctrl-B" {
		t.Fatalf("prefix = %v/%q, want Ctrl-B", cfg.PrefixKey, cfg.PrefixLabel)
	}
	if !cfg.Mouse {
		t.Fatal("mouse should be on from file")
	}
	if cfg.StatusFg != tcell.PaletteColor(250) {
		t.Fatalf("status_fg = %v, want palette 250", cfg.StatusFg)
	}
	if cfg.StatusBg != tcell.NewRGBColor(0x10, 0x14, 0x18) {
		t.Fatalf("status_bg = %v, want #101418", cfg.StatusBg)
	}
	if cfg.PaneGap != 2 {
		t.Fatalf("pane_gap = %d, want 2", cfg.PaneGap)
	}
}

// TestLoadEnvOverrides verifies TATAMI_<NAME> wins over the file.
func TestLoadEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tatami.conf")
	if err := os.WriteFile(path, []byte("prefix = ctrl-a\nmouse = off\npane_gap = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TATAMI_CONFIG", path)
	t.Setenv("TATAMI_PREFIX", "ctrl-c")
	t.Setenv("TATAMI_MOUSE", "true")
	t.Setenv("TATAMI_PANE_GAP", "4")

	cfg := Load()
	if cfg.PrefixKey != tcell.KeyCtrlC {
		t.Fatalf("prefix = %v, want Ctrl-C", cfg.PrefixKey)
	}
	if !cfg.Mouse {
		t.Fatal("env TATAMI_MOUSE should override the file")
	}
	if cfg.PaneGap != 4 {
		t.Fatalf("pane_gap = %d, want 4 (env override)", cfg.PaneGap)
	}
}
