package falkor

import (
	"fmt"

	"github.com/atharva-0123-cs/tatami/layout"
	"github.com/atharva-0123-cs/tatami/window"
)

// LayoutToCypher generates a series of Cypher statements to represent the current layout tree.
func LayoutToCypher(wm *layout.WindowManager) []string {
	var statements []string
	
	// Clear existing graph (optional: for session persistence we might only update)
	statements = append(statements, "GRAPH.QUERY tatami 'MATCH (n) DETACH DELETE n'")

	// Create Window
	statements = append(statements, fmt.Sprintf("GRAPH.QUERY tatami 'CREATE (:Window {id: \"win-%d\", name: \"%s\"})'", wm.WindowIndex(), wm.WindowName()))
	
	// Start recursive tree mapping
	if root := wm.Root(); root != nil {
		nodeID := "node-0"
		statements = append(statements, fmt.Sprintf("GRAPH.QUERY tatami 'MATCH (w:Window {id: \"win-%d\"}) CREATE (n:Node {id: \"%s\"}) CREATE (w)-[:ROOT]->(n)'", wm.WindowIndex(), nodeID))
		statements = append(statements, mapNode(nodeID, root)...)
	}
	
	return statements
}

func mapNode(nodeID string, n *window.Node) []string {
	var stmts []string
	
	for i, child := range n.Children {
		childID := fmt.Sprintf("%s-c%d", nodeID, i)
		
		if child.Leaf {
			// Map Pane
			stmts = append(stmts, fmt.Sprintf("GRAPH.QUERY tatami 'MATCH (p:Node {id: \"%s\"}) CREATE (c:Pane {id: \"%s\", name: \"%s\"}) CREATE (p)-[:CHILD]->(c)'", nodeID, childID, child.Pane.Name()))
		} else {
			// Map Container Node
			stmts = append(stmts, fmt.Sprintf("GRAPH.QUERY tatami 'MATCH (p:Node {id: \"%s\"}) CREATE (c:Node {id: \"%s\", type: \"container\"}) CREATE (p)-[:CHILD]->(c)'", nodeID, childID))
			stmts = append(stmts, mapNode(childID, child)...)
		}
	}
	
	return stmts
}
