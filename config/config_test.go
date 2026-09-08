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

// TestLoadFile exercises file resolution through ROGE_CONFIG and key parsing,
// including comments, blank lines and unknown keys.
func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roge.conf")
	content := "# roge demo config\n\nprefix = ctrl-b\nmouse = on\nstatus_bg = #101418\nstatus_fg = 250\ndefault = ignored\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROGE_CONFIG", path)
	t.Setenv("ROGE_PREFIX", "")
	t.Setenv("ROGE_MOUSE", "")

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
}

// TestLoadEnvOverrides verifies ROGE_<NAME> wins over the file.
func TestLoadEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roge.conf")
	if err := os.WriteFile(path, []byte("prefix = ctrl-a\nmouse = off\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROGE_CONFIG", path)
	t.Setenv("ROGE_PREFIX", "ctrl-c")
	t.Setenv("ROGE_MOUSE", "true")

	cfg := Load()
	if cfg.PrefixKey != tcell.KeyCtrlC {
		t.Fatalf("prefix = %v, want Ctrl-C", cfg.PrefixKey)
	}
	if !cfg.Mouse {
		t.Fatal("env ROGE_MOUSE should override the file")
	}
}
