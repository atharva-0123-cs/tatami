package falkor

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
)

// ScanStats summarizes a project scan and (when ingestion ran) the number of
// Cypher statements written.
type ScanStats struct {
	Root       string
	Files      int
	Skipped    int
	Nodes      int
	Edges      int
	Calls      int
	Statements int
	ByLanguage map[string]int
}

// ScanProject walks root, parses every supported source file and returns the
// merged code graph plus statistics. It never touches FalkorDB; call
// Client.Ingest (or Client.IndexProject) to persist the result. Unreadable or
// unparsable files are counted in Skipped and do not abort the scan.
func ScanProject(root string) (*Graph, ScanStats, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	stats := ScanStats{Root: absRoot, ByLanguage: map[string]int{}}
	g := NewGraph()

	projID := projectNodeID(absRoot)
	g.AddNode(Node{ID: projID, Label: LabelProject, Props: map[string]string{
		"name": filepath.Base(absRoot),
		"path": slash(absRoot),
	}})

	walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			stats.Skipped++
			return nil
		}
		if d.IsDir() {
			if path != absRoot && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		parser := ParserFor(d.Name())
		if parser == nil {
			return nil
		}
		rel, rerr := filepath.Rel(absRoot, path)
		if rerr != nil {
			rel = d.Name()
		}
		src := readSource(path)
		if src == nil {
			stats.Skipped++
			return nil
		}
		frag, perr := parser.Parse(rel, src)
		if perr != nil {
			slog.Warn("falkor: skipping unparsable file", "file", rel, "err", perr)
			stats.Skipped++
			return nil
		}
		g.Merge(frag)
		g.AddEdge(projID, fileNodeID(rel), RelContains)
		g.AddEdge(projID, packageNodeID(dirOf(rel)), RelContains)
		stats.Files++
		stats.ByLanguage[fragmentLanguage(frag, parser)]++
		return nil
	})
	if walkErr != nil {
		return nil, stats, walkErr
	}

	stats.Calls = g.Resolve()
	stats.Nodes = g.NodeCount()
	stats.Edges = g.EdgeCount()
	return g, stats, nil
}

// fragmentLanguage reports the language recorded on the fragment's File node,
// falling back to the parser's static tag (JS/TS share one parser but tag
// files differently by extension).
func fragmentLanguage(frag *Graph, parser Parser) string {
	for _, n := range frag.Nodes() {
		if n.Label == LabelFile {
			if lang := n.Props["language"]; lang != "" {
				return lang
			}
		}
	}
	return parser.Language()
}

// Clear removes every node and relationship from the client's graph.
func (c *Client) Clear(ctx context.Context) error {
	_, err := c.Query(ctx, "MATCH (n) DETACH DELETE n")
	return err
}

// Ingest clears the graph and writes every node and edge of g as Cypher. It
// returns the number of statements executed. Statements are ordered so node
// MERGEs precede relationship MATCHes, making the write idempotent.
func (c *Client) Ingest(ctx context.Context, g *Graph) (int, error) {
	if err := c.Clear(ctx); err != nil {
		return 0, fmt.Errorf("clearing graph: %w", err)
	}
	stmts := g.Cypher()
	for i, stmt := range stmts {
		if _, err := c.Query(ctx, stmt); err != nil {
			return i, fmt.Errorf("ingesting statement %d/%d: %w", i+1, len(stmts), err)
		}
	}
	return len(stmts), nil
}

// IndexProject scans root and ingests the resulting graph in one call.
func (c *Client) IndexProject(ctx context.Context, root string) (ScanStats, error) {
	g, stats, err := ScanProject(root)
	if err != nil {
		return stats, err
	}
	n, err := c.Ingest(ctx, g)
	if err != nil {
		return stats, err
	}
	stats.Statements = n
	return stats, nil
}
