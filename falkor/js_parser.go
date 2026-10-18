package falkor

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// JSParser builds a code graph from JavaScript / TypeScript source using
// regular-expression scanning. It recognises ES module imports, CommonJS
// require calls, classes, function declarations, arrow/function expressions
// assigned to constants, and call sites inside them. As with PythonParser this
// is a heuristic scanner, not a full parser, but it is enough for dependency
// and call-chain queries.
type JSParser struct{}

// Language implements Parser.
func (p *JSParser) Language() string { return "javascript" }

// Extensions implements Parser.
func (p *JSParser) Extensions() []string {
	return []string{".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"}
}

var (
	jsImportFromRe = regexp.MustCompile(`(?m)^\s*import\b[^;\n]*?from\s*['"]([^'"]+)['"]`)
	jsImportRe     = regexp.MustCompile(`(?m)^\s*import\s*['"]([^'"]+)['"]`)
	jsRequireRe    = regexp.MustCompile(`require\(\s*['"]([^'"]+)['"]\s*\)`)
	jsExportRe     = regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?`)
	jsClassRe      = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:default\s+)?class\s+([A-Za-z_$][\w$]*)`)
	jsFuncRe       = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*\(`)
	jsMethodRe     = regexp.MustCompile(`(?m)^\s*(?:(?:public|private|protected|static|async|get|set)\s+)*([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*\{`)
	jsArrowRe      = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*=>`)
	jsAssignFnRe   = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?function\b`)
	jsCallRe       = regexp.MustCompile(`([A-Za-z_$][\w$]*)\s*\(`)
)

// jsKeywords are names jsCallRe would otherwise mistake for calls.
var jsKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true,
	"return": true, "function": true, "typeof": true, "new": true, "await": true,
	"class": true, "import": true, "require": true, "console": true,
	"super": true, "this": true, "else": true, "do": true,
}

// Parse implements Parser.
func (p *JSParser) Parse(relPath string, src []byte) (*Graph, error) {
	g := NewGraph()
	fileID := fileNodeID(relPath)
	dir := dirOf(relPath)
	pkgID := packageNodeID(dir)
	text := string(src)

	g.AddNode(Node{ID: pkgID, Label: LabelPackage, Props: map[string]string{
		"name": dir, "path": dir,
	}})
	g.AddNode(Node{ID: fileID, Label: LabelFile, Props: map[string]string{
		"path":     slash(relPath),
		"name":     filepath.Base(relPath),
		"language": languageFor(relPath),
		"package":  dir,
	}})
	g.AddEdge(fileID, pkgID, RelBelongsTo)
	g.AddEdge(pkgID, fileID, RelContains)

	for _, m := range jsImportFromRe.FindAllStringSubmatch(text, -1) {
		p.addImport(g, fileID, m[1])
	}
	for _, m := range jsImportRe.FindAllStringSubmatch(text, -1) {
		p.addImport(g, fileID, m[1])
	}
	for _, m := range jsRequireRe.FindAllStringSubmatch(text, -1) {
		p.addImport(g, fileID, m[1])
	}

	// Declarations: record the line each starts on so call scanning can
	// attribute calls to the enclosing declaration.
	type decl struct {
		line int
		id   string
	}
	var decls []decl
	add := func(line int, name string, label string, kind string) {
		id := symbolNodeID(relPath, fnKind(label), name)
		g.AddNode(Node{ID: id, Label: label, Props: map[string]string{
			"name": name, "kind": kind, "file": fileID, "line": strconv.Itoa(line),
			"exported": strconv.FormatBool(exportedAt(text, line)),
		}})
		g.AddEdge(fileID, id, RelContains)
		decls = append(decls, decl{line: line, id: id})
	}

	for _, m := range jsClassRe.FindAllStringSubmatchIndex(text, -1) {
		name := text[m[2]:m[3]]
		add(lineOf(text, m[0]), name, LabelStruct, "class")
	}
	for _, m := range jsFuncRe.FindAllStringSubmatchIndex(text, -1) {
		name := text[m[2]:m[3]]
		add(lineOf(text, m[0]), name, LabelFunction, "function")
	}
	for _, m := range jsMethodRe.FindAllStringSubmatchIndex(text, -1) {
		name := text[m[2]:m[3]]
		if jsKeywords[name] {
			continue
		}
		add(lineOf(text, m[0]), name, LabelFunction, "method")
	}
	for _, m := range jsArrowRe.FindAllStringSubmatchIndex(text, -1) {
		name := text[m[2]:m[3]]
		add(lineOf(text, m[0]), name, LabelFunction, "arrow")
	}
	for _, m := range jsAssignFnRe.FindAllStringSubmatchIndex(text, -1) {
		name := text[m[2]:m[3]]
		add(lineOf(text, m[0]), name, LabelFunction, "function")
	}

	// Attribute each call to the nearest preceding declaration. The first line
	// (module scope) attributes to nothing.
	calls := jsCallRe.FindAllStringSubmatchIndex(text, -1)
	for _, m := range calls {
		line := lineOf(text, m[0])
		callee := text[m[2]:m[3]]
		if jsKeywords[callee] {
			continue
		}
		owner := ""
		ownerLine := -1
		for _, d := range decls {
			if d.line <= line && d.line > ownerLine {
				owner, ownerLine = d.id, d.line
			}
		}
		if owner != "" {
			g.AddCall(owner, fileID, callee)
		}
	}
	return g, nil
}

// addImport records an import target.
func (p *JSParser) addImport(g *Graph, fileID, importPath string) {
	if importPath == "" {
		return
	}
	impID := importNodeID(importPath)
	g.AddNode(Node{ID: impID, Label: LabelImport, Props: map[string]string{"path": importPath}})
	g.AddEdge(fileID, impID, RelImports)
}

// fnKind maps a node label to the ID scheme segment ("func" or "type").
func fnKind(label string) string {
	if label == LabelStruct {
		return "type"
	}
	return "func"
}

// languageFor distinguishes TypeScript from JavaScript by extension.
func languageFor(relPath string) string {
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".ts", ".tsx":
		return "typescript"
	default:
		return "javascript"
	}
}

// lineOf returns the 1-based line number containing byte offset off.
func lineOf(text string, off int) int { return strings.Count(text[:off], "\n") + 1 }

// exportedAt reports whether the declaration on the given line is exported.
func exportedAt(text string, line int) bool {
	lines := strings.SplitN(text, "\n", line+1)
	if line-1 < 0 || line-1 >= len(lines) {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(lines[line-1]), "export")
}
