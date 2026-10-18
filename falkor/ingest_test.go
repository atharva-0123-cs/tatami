package falkor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanProject(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nfunc main() { helper() }\nfunc helper() {}\n")
	write("pkg/a.py", "def a():\n    b()\n\ndef b():\n    pass\n")
	write("web/app.ts", "export function boot() {}\n")
	write("vendor/skip.go", "package vendor\n")
	write(".hidden/skip.go", "package hidden\n")

	g, stats, err := ScanProject(dir)
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}
	if stats.Files != 3 {
		t.Errorf("Files = %d, want 3", stats.Files)
	}
	if stats.ByLanguage["go"] != 1 || stats.ByLanguage["python"] != 1 || stats.ByLanguage["typescript"] != 1 {
		t.Errorf("ByLanguage = %v", stats.ByLanguage)
	}
	if !hasNode(g, LabelProject, "path", slash(dir)) {
		t.Errorf("missing Project node")
	}
	if hasNode(g, LabelFile, "path", "vendor/skip.go") {
		t.Errorf("vendor directory should be skipped")
	}
	if hasNode(g, LabelFile, "path", ".hidden/skip.go") {
		t.Errorf("hidden directory should be skipped")
	}
	// main -> helper resolves.
	mainID := symbolNodeID("main.go", "func", "main")
	helperID := symbolNodeID("main.go", "func", "helper")
	if !hasEdge(g, mainID, helperID, RelCalls) {
		t.Errorf("expected main -CALLS-> helper after scan")
	}
}

func TestSupportedExtensions(t *testing.T) {
	exts := SupportedExtensions()
	for _, want := range []string{".go", ".py", ".ts", ".tsx", ".js"} {
		found := false
		for _, e := range exts {
			if e == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing extension %q in %v", want, exts)
		}
	}
}
