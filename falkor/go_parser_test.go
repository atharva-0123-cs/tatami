package falkor

import (
	"strings"
	"testing"
)

const sampleGo = `package demo

import (
	"fmt"
	"strings"
)

// Greeter says hello.
type Greeter struct {
	Name    string
	private int
}

// Greet prints a greeting.
func (g *Greeter) Greet() {
	fmt.Println(strings.ToUpper(g.Name))
}

func main() {
	g := &Greeter{Name: "tatami"}
	g.Greet()
	helper()
}

func helper() {
	fmt.Println("helper")
}
`

func TestGoParserNodes(t *testing.T) {
	g, err := (&GoParser{}).Parse("demo/demo.go", []byte(sampleGo))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// The package and file nodes plus a BELONGS_TO edge.
	if !hasNode(g, LabelFile, "path", "demo/demo.go") {
		t.Errorf("missing File node")
	}
	if !hasNode(g, LabelPackage, "name", "demo") {
		t.Errorf("missing Package node")
	}
	fileID := fileNodeID("demo/demo.go")
	if !hasEdge(g, fileID, packageNodeID("demo"), RelBelongsTo) {
		t.Errorf("missing File-BELONGS_TO->Package edge")
	}

	// Imports.
	for _, imp := range []string{"fmt", "strings"} {
		if !hasNode(g, LabelImport, "path", imp) {
			t.Errorf("missing Import node %q", imp)
		}
		if !hasEdge(g, fileID, importNodeID(imp), RelImports) {
			t.Errorf("missing IMPORTS edge to %q", imp)
		}
	}

	// Declarations.
	if !hasNode(g, LabelStruct, "name", "Greeter") {
		t.Errorf("missing Struct Greeter")
	}
	if !hasNode(g, LabelField, "name", "Name") || !hasNode(g, LabelField, "name", "private") {
		t.Errorf("missing struct fields")
	}
	if !hasNode(g, LabelFunction, "qualified", "Greeter.Greet") {
		t.Errorf("missing method Greeter.Greet")
	}
	if !hasNode(g, LabelFunction, "qualified", "main") {
		t.Errorf("missing function main")
	}
	if !hasNode(g, LabelFunction, "qualified", "helper") {
		t.Errorf("missing function helper")
	}
}

func TestGoParserCallResolution(t *testing.T) {
	g, err := (&GoParser{}).Parse("demo/demo.go", []byte(sampleGo))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// g.Greet() and main -> helper should resolve to local functions.
	resolved := g.Resolve()
	if resolved == 0 {
		t.Fatalf("expected resolved calls, got 0")
	}
	mainID := symbolNodeID("demo/demo.go", "func", "main")
	helperID := symbolNodeID("demo/demo.go", "func", "helper")
	if !hasEdge(g, mainID, helperID, RelCalls) {
		t.Errorf("expected main -CALLS-> helper")
	}
}

func TestGoParserSyntaxError(t *testing.T) {
	if _, err := (&GoParser{}).Parse("bad.go", []byte("package x\nfunc (")); err == nil {
		t.Fatalf("expected parse error for malformed source")
	}
}

func hasNode(g *Graph, label, key, value string) bool {
	for _, n := range g.Nodes() {
		if n.Label == label && n.Props[key] == value {
			return true
		}
	}
	return false
}

func hasEdge(g *Graph, from, to, rel string) bool {
	for _, e := range g.Edges() {
		if e.From == from && e.To == to && e.Rel == rel {
			return true
		}
	}
	return false
}

func TestGoParserDeterministic(t *testing.T) {
	a, _ := (&GoParser{}).Parse("demo/demo.go", []byte(sampleGo))
	b, _ := (&GoParser{}).Parse("demo/demo.go", []byte(sampleGo))
	ca := strings.Join(a.Cypher(), "\n")
	cb := strings.Join(b.Cypher(), "\n")
	if ca != cb {
		t.Errorf("Cypher output is not deterministic")
	}
}
