package falkor

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Parser extracts a code graph from a single source file. Implementations are
// stateless and safe to reuse across files; they must not touch the network or
// the filesystem (the source bytes are supplied by the scanner).
type Parser interface {
	// Language is the lowercase language tag stored on File nodes.
	Language() string
	// Extensions lists the file suffixes this parser handles (e.g. ".go").
	Extensions() []string
	// Parse converts one file into graph fragments. relPath is the file path
	// relative to the project root and is used to derive stable node IDs.
	Parse(relPath string, src []byte) (*Graph, error)
}

// parsers is the registry consulted by parserFor. Order is irrelevant because
// extensions are disjoint.
var parsers = []Parser{
	&GoParser{},
	&PythonParser{},
	&JSParser{},
}

// ParserFor returns the parser registered for a path, or nil when the
// extension is not handled.
func ParserFor(filePath string) Parser {
	ext := strings.ToLower(filepath.Ext(filePath))
	for _, p := range parsers {
		for _, e := range p.Extensions() {
			if e == ext {
				return p
			}
		}
	}
	return nil
}

// SupportedExtensions returns every extension the registry understands,
// sorted for stable display.
func SupportedExtensions() []string {
	seen := map[string]bool{}
	for _, p := range parsers {
		for _, e := range p.Extensions() {
			seen[e] = true
		}
	}
	out := make([]string, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// Node ID helpers. IDs are scheme-prefixed and rooted at the project-relative
// path so the same tree always maps to the same nodes.

func projectNodeID(root string) string  { return "project://" + slash(root) }
func packageNodeID(dir string) string   { return "package://" + slash(dir) }
func fileNodeID(relPath string) string  { return "file://" + slash(relPath) }
func importNodeID(imp string) string    { return "import://" + imp }

// symbolNodeID builds the ID for a declaration inside a file, e.g.
// "func://cmd/main.go:main" or "type://internal/app/app.go:App".
func symbolNodeID(relPath, kind, name string) string {
	return fmt.Sprintf("%s://%s:%s", kind, slash(relPath), name)
}

// dirOf returns the slash-separated directory of a relative path ("." for the
// project root).
func dirOf(relPath string) string {
	d := path.Dir(slash(relPath))
	if d == "" {
		return "."
	}
	return d
}

// slash normalizes OS paths to forward slashes so node IDs are host-portable.
func slash(p string) string { return filepath.ToSlash(p) }

// skipDir reports whether a directory should be excluded from indexing.
func skipDir(name string) bool {
	switch name {
	case "vendor", "node_modules", "dist", "build", "target", "__pycache__":
		return true
	}
	// Hidden directories (.git, .venv, ...) are skipped, but the root itself
	// is never passed here.
	return strings.HasPrefix(name, ".")
}

// readSource reads a file, returning nil (no error) when it cannot be read so a
// single unreadable file never aborts a whole index.
func readSource(filePath string) []byte {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	return b
}
