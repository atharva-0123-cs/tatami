package falkor

import (
	"fmt"
	"sort"
	"strings"
)

// Node is a vertex in the code knowledge graph.
//
// ID is the stable, globally unique key used by edges and by MERGE; Label is
// the FalkorDB node label (see code_schema.go); Props are the node's scalar
// properties. IDs are derived from paths relative to the indexed project root
// so re-indexing the same tree produces the same graph.
type Node struct {
	ID    string
	Label string
	Props map[string]string
}

// Edge is a directed relationship between two nodes, referenced by node ID.
type Edge struct {
	From string
	To   string
	Rel  string
}

// CallRef is an unresolved call site: the function node that contains the call
// and the simple (or selector) name being called. Resolution against the
// project-wide symbol table happens in Resolve, once every file is parsed.
type CallRef struct {
	From string // caller node ID
	File string // file node ID, for same-file precedence
	Name string // callee simple name
}

// Graph is an in-memory accumulation of nodes and edges produced by the
// language parsers and later serialized to Cypher. The zero value is not
// usable; use NewGraph.
type Graph struct {
	nodes map[string]*Node
	edges []Edge
	calls []CallRef
}

// NewGraph returns an empty graph.
func NewGraph() *Graph {
	return &Graph{nodes: make(map[string]*Node)}
}

// AddNode inserts n, merging properties into an existing node with the same ID.
func (g *Graph) AddNode(n Node) {
	if n.ID == "" || n.Label == "" {
		return
	}
	if n.Props == nil {
		n.Props = map[string]string{}
	}
	if existing, ok := g.nodes[n.ID]; ok {
		for k, v := range n.Props {
			existing.Props[k] = v
		}
		return
	}
	cp := n
	g.nodes[n.ID] = &cp
}

// AddEdge records a directed relationship. Duplicate (from, to, rel) edges are
// collapsed so re-parsing never inflates the graph.
func (g *Graph) AddEdge(from, to, rel string) {
	if from == "" || to == "" || rel == "" {
		return
	}
	for _, e := range g.edges {
		if e.From == from && e.To == to && e.Rel == rel {
			return
		}
	}
	g.edges = append(g.edges, Edge{From: from, To: to, Rel: rel})
}

// AddCall records an unresolved call site for later resolution.
func (g *Graph) AddCall(from, file, name string) {
	if from == "" || name == "" {
		return
	}
	g.calls = append(g.calls, CallRef{From: from, File: file, Name: name})
}

// Merge folds every node, edge and pending call of other into g.
func (g *Graph) Merge(other *Graph) {
	if other == nil {
		return
	}
	for _, n := range other.Nodes() {
		g.AddNode(*n)
	}
	for _, e := range other.Edges() {
		g.AddEdge(e.From, e.To, e.Rel)
	}
	g.calls = append(g.calls, other.calls...)
}

// Nodes returns the nodes sorted by ID for deterministic output.
func (g *Graph) Nodes() []*Node {
	out := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Edges returns the edges in insertion order (parsers are deterministic, so
// the order is stable across runs).
func (g *Graph) Edges() []Edge { return g.edges }

// NodeCount returns the number of distinct nodes.
func (g *Graph) NodeCount() int { return len(g.nodes) }

// EdgeCount returns the number of distinct edges.
func (g *Graph) EdgeCount() int { return len(g.edges) }

// Resolve turns pending CallRefs into CALLS edges using the graph's symbol
// table. Same-file functions win; otherwise every function with a matching
// simple name is linked. Unresolved names (stdlib, third-party) are ignored.
func (g *Graph) Resolve() int {
	type sym struct {
		id   string
		file string
	}
	byName := map[string][]sym{}
	for _, n := range g.nodes {
		if n.Label != LabelFunction {
			continue
		}
		byName[n.Props["name"]] = append(byName[n.Props["name"]], sym{id: n.ID, file: n.Props["file"]})
	}

	resolved := 0
	for _, c := range g.calls {
		cands, ok := byName[c.Name]
		if !ok {
			continue
		}
		// Prefer call targets declared in the same file.
		var target string
		for _, cand := range cands {
			if cand.file == c.File {
				target = cand.id
				break
			}
		}
		if target == "" && len(cands) > 0 {
			target = cands[0].id
		}
		if target == "" || target == c.From {
			continue
		}
		g.AddEdge(c.From, target, RelCalls)
		resolved++
	}
	return resolved
}

// Cypher serializes the graph into idempotent Cypher statements. All node
// statements come before edge statements so a MATCH for an endpoint always
// succeeds. Statements are deterministic and safe to replay.
func (g *Graph) Cypher() []string {
	nodes := g.Nodes()
	stmts := make([]string, 0, len(nodes)+len(g.edges))
	for _, n := range nodes {
		stmts = append(stmts, nodeStatement(n))
	}
	for _, e := range g.edges {
		from := g.nodes[e.From]
		to := g.nodes[e.To]
		if from == nil || to == nil {
			continue
		}
		stmts = append(stmts, fmt.Sprintf(
			"MATCH (a:%s {id: \"%s\"}), (b:%s {id: \"%s\"}) MERGE (a)-[:%s]->(b)",
			from.Label, cypherString(from.ID), to.Label, cypherString(to.ID), e.Rel,
		))
	}
	return stmts
}

// nodeStatement renders a single MERGE plus sorted SET assignments.
func nodeStatement(n *Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "MERGE (n:%s {id: \"%s\"})", n.Label, cypherString(n.ID))
	keys := make([]string, 0, len(n.Props))
	for k := range n.Props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, " SET n.%s = \"%s\"", k, cypherString(n.Props[k]))
	}
	return b.String()
}

// cypherString escapes a value for use inside a double-quoted Cypher literal.
func cypherString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
