package falkor

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// PythonParser builds a code graph from Python source using a lightweight
// line/indentation scanner rather than a full grammar. It recognises top-level
// and nested `def`/`class` declarations, `import`/`from ... import` statements
// and call sites inside function bodies. This is intentionally heuristic: it is
// enough to answer dependency and call-chain questions without a Python
// toolchain.
type PythonParser struct{}

// Language implements Parser.
func (p *PythonParser) Language() string { return "python" }

// Extensions implements Parser.
func (p *PythonParser) Extensions() []string { return []string{".py"} }

var (
	pyClassRe  = regexp.MustCompile(`^(\s*)class\s+([A-Za-z_]\w*)`)
	pyDefRe    = regexp.MustCompile(`^(\s*)def\s+([A-Za-z_]\w*)\s*\(`)
	pyFromRe   = regexp.MustCompile(`^\s*from\s+([\w.]+)\s+import\s+(.+)$`)
	pyImportRe = regexp.MustCompile(`^\s*import\s+(.+)$`)
	pyCallRe   = regexp.MustCompile(`([A-Za-z_]\w*)\s*\(`)
)

// pyKeywords are names pyCallRe would otherwise mistake for calls.
var pyKeywords = map[string]bool{
	"if": true, "elif": true, "while": true, "for": true, "with": true,
	"return": true, "yield": true, "assert": true, "lambda": true,
	"except": true, "in": true, "not": true, "and": true, "or": true,
	"print": true, "class": true, "def": true, "raise": true, "del": true,
}

// Parse implements Parser.
func (p *PythonParser) Parse(relPath string, src []byte) (*Graph, error) {
	g := NewGraph()
	fileID := fileNodeID(relPath)
	dir := dirOf(relPath)
	pkgID := packageNodeID(dir)

	g.AddNode(Node{ID: pkgID, Label: LabelPackage, Props: map[string]string{
		"name": dir, "path": dir,
	}})
	g.AddNode(Node{ID: fileID, Label: LabelFile, Props: map[string]string{
		"path":     slash(relPath),
		"name":     filepath.Base(relPath),
		"language": "python",
		"package":  dir,
	}})
	g.AddEdge(fileID, pkgID, RelBelongsTo)
	g.AddEdge(pkgID, fileID, RelContains)

	currentFunc := ""
	funcIndent := -1

	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if m := pyFromRe.FindStringSubmatch(line); m != nil {
			p.addImport(g, fileID, m[1])
			continue
		}
		if m := pyImportRe.FindStringSubmatch(line); m != nil {
			for _, part := range strings.Split(m[1], ",") {
				name := strings.TrimSpace(strings.SplitN(part, " as ", 2)[0])
				if name != "" {
					p.addImport(g, fileID, name)
				}
			}
			continue
		}
		if m := pyClassRe.FindStringSubmatch(line); m != nil {
			name := m[2]
			id := symbolNodeID(relPath, "type", name)
			g.AddNode(Node{ID: id, Label: LabelStruct, Props: map[string]string{
				"name": name, "kind": "class", "file": fileID, "line": strconv.Itoa(i + 1),
			}})
			g.AddEdge(fileID, id, RelContains)
			continue
		}
		if m := pyDefRe.FindStringSubmatch(line); m != nil {
			indent := indentWidth(m[1])
			name := m[2]
			id := symbolNodeID(relPath, "func", name)
			g.AddNode(Node{ID: id, Label: LabelFunction, Props: map[string]string{
				"name": name, "qualified": name, "file": fileID,
				"line":     strconv.Itoa(i + 1),
				"exported": strconv.FormatBool(!strings.HasPrefix(name, "_")),
			}})
			g.AddEdge(fileID, id, RelContains)
			currentFunc, funcIndent = id, indent
			continue
		}

		if currentFunc != "" && indentWidth(line) > funcIndent {
			for _, m := range pyCallRe.FindAllStringSubmatch(line, -1) {
				callee := m[1]
				if pyKeywords[callee] {
					continue
				}
				g.AddCall(currentFunc, fileID, callee)
			}
		}
	}
	return g, nil
}

// addImport records an import target as a shared Import node plus an edge.
func (p *PythonParser) addImport(g *Graph, fileID, importPath string) {
	if importPath == "" {
		return
	}
	impID := importNodeID(importPath)
	g.AddNode(Node{ID: impID, Label: LabelImport, Props: map[string]string{"path": importPath}})
	g.AddEdge(fileID, impID, RelImports)
}

// indentWidth counts leading whitespace (tab = one column).
func indentWidth(line string) int {
	n := 0
	for _, r := range line {
		if r == ' ' || r == '\t' {
			n++
			continue
		}
		break
	}
	return n
}
