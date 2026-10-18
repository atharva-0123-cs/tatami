package falkor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strconv"
	"strings"
)

// GoParser builds a code graph from Go source using the standard library's
// go/ast. It records files, packages, imports, functions, methods, structs
// (and other named types), struct fields and variables, plus call sites
// (resolved later by Graph.Resolve).
type GoParser struct{}

// Language implements Parser.
func (p *GoParser) Language() string { return "go" }

// Extensions implements Parser.
func (p *GoParser) Extensions() []string { return []string{".go"} }

// Parse implements Parser. relPath is project-relative and used for node IDs.
func (p *GoParser) Parse(relPath string, src []byte) (*Graph, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, relPath, src, parser.AllErrors)
	if err != nil {
		return nil, err
	}

	g := NewGraph()
	fileID := fileNodeID(relPath)
	dir := dirOf(relPath)
	pkgID := packageNodeID(dir)

	g.AddNode(Node{ID: pkgID, Label: LabelPackage, Props: map[string]string{
		"name": f.Name.Name,
		"path": dir,
	}})
	g.AddNode(Node{ID: fileID, Label: LabelFile, Props: map[string]string{
		"path":     slash(relPath),
		"name":     filepath.Base(relPath),
		"language": "go",
		"package":  f.Name.Name,
	}})
	g.AddEdge(fileID, pkgID, RelBelongsTo)
	g.AddEdge(pkgID, fileID, RelContains)

	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		impID := importNodeID(importPath)
		g.AddNode(Node{ID: impID, Label: LabelImport, Props: map[string]string{
			"path":  importPath,
			"alias": alias,
		}})
		g.AddEdge(fileID, impID, RelImports)
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			p.parseFunc(g, fset, d, relPath, fileID)
		case *ast.GenDecl:
			p.parseGenDecl(g, fset, d, relPath, fileID)
		}
	}
	return g, nil
}

// parseFunc records a function or method and every call site in its body.
func (p *GoParser) parseFunc(g *Graph, fset *token.FileSet, d *ast.FuncDecl, relPath, fileID string) {
	name := d.Name.Name
	receiver := ""
	if d.Recv != nil && len(d.Recv.List) > 0 {
		receiver = baseTypeName(d.Recv.List[0].Type)
	}
	qualified := name
	if receiver != "" {
		qualified = receiver + "." + name
	}

	fnID := symbolNodeID(relPath, "func", qualified)
	line := fset.Position(d.Pos()).Line
	g.AddNode(Node{ID: fnID, Label: LabelFunction, Props: map[string]string{
		"name":      name,
		"qualified": qualified,
		"receiver":  receiver,
		"file":      fileID,
		"line":      strconv.Itoa(line),
		"exported":  strconv.FormatBool(d.Name.IsExported()),
	}})
	g.AddEdge(fileID, fnID, RelContains)

	if d.Body == nil {
		return
	}
	ast.Inspect(d.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			g.AddCall(fnID, fileID, calleeName(call.Fun))
		}
		return true
	})
}

// parseGenDecl records named types (structs, interfaces, aliases) with their
// fields, plus package-level variables and constants. Imports are handled from
// f.Imports, so token.IMPORT is ignored here.
func (p *GoParser) parseGenDecl(g *Graph, fset *token.FileSet, d *ast.GenDecl, relPath, fileID string) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			name := s.Name.Name
			typeID := symbolNodeID(relPath, "type", name)
			kind := "other"
			switch t := s.Type.(type) {
			case *ast.StructType:
				kind = "struct"
				p.parseStructFields(g, t, name, typeID, relPath)
			case *ast.InterfaceType:
				kind = "interface"
			case *ast.Ident, *ast.SelectorExpr:
				kind = "alias"
			}
			g.AddNode(Node{ID: typeID, Label: LabelStruct, Props: map[string]string{
				"name":     name,
				"kind":     kind,
				"file":     fileID,
				"line":     strconv.Itoa(fset.Position(s.Pos()).Line),
				"exported": strconv.FormatBool(s.Name.IsExported()),
			}})
			g.AddEdge(fileID, typeID, RelContains)

		case *ast.ValueSpec:
			kind := "var"
			if d.Tok == token.CONST {
				kind = "const"
			}
			for _, ident := range s.Names {
				if ident.Name == "_" {
					continue
				}
				varID := symbolNodeID(relPath, "var", ident.Name)
				g.AddNode(Node{ID: varID, Label: LabelVariable, Props: map[string]string{
					"name": ident.Name,
					"kind": kind,
					"file": fileID,
					"line": strconv.Itoa(fset.Position(ident.Pos()).Line),
				}})
				g.AddEdge(fileID, varID, RelContains)
			}
		}
	}
}

// parseStructFields records each named field of a struct type.
func (p *GoParser) parseStructFields(g *Graph, st *ast.StructType, structName, typeID, relPath string) {
	if st.Fields == nil {
		return
	}
	for _, field := range st.Fields.List {
		typeStr := types.ExprString(field.Type)
		names := field.Names
		if len(names) == 0 {
			// Embedded field: use the base type name as the field name.
			base := baseTypeName(field.Type)
			if base == "" {
				continue
			}
			fieldID := symbolNodeID(relPath, "field", structName+"."+base)
			g.AddNode(Node{ID: fieldID, Label: LabelField, Props: map[string]string{
				"name": base, "type": typeStr, "struct": structName, "embedded": "true",
			}})
			g.AddEdge(typeID, fieldID, RelHasField)
			continue
		}
		for _, nm := range names {
			fieldID := symbolNodeID(relPath, "field", structName+"."+nm.Name)
			g.AddNode(Node{ID: fieldID, Label: LabelField, Props: map[string]string{
				"name": nm.Name, "type": typeStr, "struct": structName,
			}})
			g.AddEdge(typeID, fieldID, RelHasField)
		}
	}
}

// calleeName extracts the called name from a call expression, unwrapping
// selectors, generic instantiations and parentheses.
func calleeName(expr ast.Expr) string {
	switch f := expr.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.IndexExpr:
		return calleeName(f.X)
	case *ast.IndexListExpr:
		return calleeName(f.X)
	case *ast.ParenExpr:
		return calleeName(f.X)
	}
	return ""
}

// baseTypeName returns the bare identifier of a type expression, stripping
// pointers, slices, generic arguments and package qualifiers.
func baseTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return baseTypeName(t.X)
	case *ast.IndexExpr:
		return baseTypeName(t.X)
	case *ast.IndexListExpr:
		return baseTypeName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.ParenExpr:
		return baseTypeName(t.X)
	}
	return ""
}
