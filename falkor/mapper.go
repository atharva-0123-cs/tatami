package falkor

import (
	"fmt"
	"strconv"

	"github.com/atharva-0123-cs/tatami/layout"
	"github.com/atharva-0123-cs/tatami/window"
)

// LayoutToCypher generates Cypher statements that represent the active
// window's layout tree (window -> root node -> panes/containers). It builds a
// Graph so node IDs and property escaping stay consistent with the code graph.
func LayoutToCypher(wm *layout.WindowManager) []string {
	g := NewGraph()

	winID := fmt.Sprintf("window://%d", wm.WindowIndex())
	g.AddNode(Node{ID: winID, Label: LabelWindow, Props: map[string]string{
		"name":  wm.WindowName(),
		"index": strconv.Itoa(wm.WindowIndex()),
	}})

	root := wm.Root()
	if root == nil {
		return g.Cypher()
	}

	rootID := "node-0"
	if root.Leaf {
		if root.Pane != nil {
			g.AddNode(Node{ID: rootID, Label: LabelPane, Props: map[string]string{"name": root.Pane.Name()}})
		}
		g.AddEdge(winID, rootID, RelRoot)
		return g.Cypher()
	}

	g.AddNode(Node{ID: rootID, Label: LabelNode, Props: map[string]string{"type": "container"}})
	g.AddEdge(winID, rootID, RelRoot)
	mapNode(g, rootID, root, rootID)
	return g.Cypher()
}

// mapNode recursively maps a container's children under parentID.
func mapNode(g *Graph, parentID string, n *window.Node, nodeID string) {
	for i, child := range n.Children {
		childID := fmt.Sprintf("%s-c%d", nodeID, i)
		if child.Leaf {
			if child.Pane == nil {
				continue
			}
			g.AddNode(Node{ID: childID, Label: LabelPane, Props: map[string]string{"name": child.Pane.Name()}})
			g.AddEdge(parentID, childID, RelChild)
			continue
		}
		g.AddNode(Node{ID: childID, Label: LabelNode, Props: map[string]string{"type": "container"}})
		g.AddEdge(parentID, childID, RelChild)
		mapNode(g, childID, child, childID)
	}
}
