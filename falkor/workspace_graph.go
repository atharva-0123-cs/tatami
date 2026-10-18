package falkor

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/atharva-0123-cs/tatami/layout"
)

// WorkspaceGraph models windows, layout trees (splits), active panes, working
// directories and process execution ancestry as nodes in FalkorDB.
type WorkspaceGraph struct {
	graph *Graph
}

// NewWorkspaceGraph creates a new workspace graph builder.
func NewWorkspaceGraph() *WorkspaceGraph {
	return &WorkspaceGraph{graph: NewGraph()}
}

// AddSession models a serialized workspace snapshot: every window, its layout
// tree (containers and panes), each pane's working directory, and the active
// window. Working directories are shared by path so two panes in the same
// directory point at one Directory node.
func (wg *WorkspaceGraph) AddSession(spec *layout.SessionSpec) {
	if spec == nil {
		return
	}
	for i, ws := range spec.Windows {
		winID := fmt.Sprintf("window://%s", ws.Name)
		props := map[string]string{
			"name":    ws.Name,
			"index":   strconv.Itoa(i),
			"active":  strconv.FormatBool(i == spec.Active),
			"session": spec.Name,
		}
		wg.graph.AddNode(Node{ID: winID, Label: LabelWindow, Props: props})

		if ws.Root == nil {
			continue
		}
		wg.mapNode(winID, "root", ws.Root, RelRoot)
	}
}

// mapNode recursively models a layout tree node, linking it to its parent with
// the given relationship. Panes connect to their working directory via
// WORKING_IN.
func (wg *WorkspaceGraph) mapNode(parentID, nodeID string, n *layout.NodeSpec, rel string) {
	if n == nil {
		return
	}
	if n.Leaf {
		paneID := parentID + "/" + nodeID
		props := map[string]string{"cwd": n.Cwd}
		if n.Pid > 0 {
			props["pid"] = strconv.Itoa(n.Pid)
		}
		wg.graph.AddNode(Node{ID: paneID, Label: LabelPane, Props: props})
		wg.graph.AddEdge(parentID, paneID, rel)
		if n.Cwd != "" {
			dirID := fmt.Sprintf("dir://%s", n.Cwd)
			wg.graph.AddNode(Node{ID: dirID, Label: LabelDirectory, Props: map[string]string{"path": n.Cwd}})
			wg.graph.AddEdge(paneID, dirID, RelWorkingIn)
		}
		if n.Pid > 0 {
			wg.AddPaneProcess(paneID, n.Pid, n.Ppid, "")
		}
		return
	}
	containerID := parentID + "/" + nodeID
	wg.graph.AddNode(Node{
		ID:    containerID,
		Label: LabelNode,
		Props: map[string]string{"type": "container", "dir": splitDirName(n.Dir)},
	})
	wg.graph.AddEdge(parentID, containerID, rel)
	for i, child := range n.Children {
		wg.mapNode(containerID, fmt.Sprintf("c%d", i), child, RelChild)
	}
}

// splitDirName renders a split direction for a graph property.
func splitDirName(dir int) string {
	if dir == 1 {
		return "vertical"
	}
	return "horizontal"
}

// AddProcessAncestry models process execution ancestry (parent_pid -> child_pid).
func (wg *WorkspaceGraph) AddProcessAncestry(pid, ppid int, cmd string) {
	childID := fmt.Sprintf("proc://%d", pid)
	wg.graph.AddNode(Node{
		ID:    childID,
		Label: LabelProcess,
		Props: map[string]string{"pid": strconv.Itoa(pid), "command": cmd},
	})
	if ppid <= 0 {
		return
	}
	parentID := fmt.Sprintf("proc://%d", ppid)
	wg.graph.AddNode(Node{
		ID:    parentID,
		Label: LabelProcess,
		Props: map[string]string{"pid": strconv.Itoa(ppid)},
	})
	wg.graph.AddEdge(parentID, childID, RelChildProcess)
}

// AddPaneProcess links a pane node to the process running its shell, and chains
// that process to its parent (the tatami process or an intermediate shell).
func (wg *WorkspaceGraph) AddPaneProcess(paneID string, pid, ppid int, cmd string) {
	wg.AddProcessAncestry(pid, ppid, cmd)
	wg.graph.AddNode(Node{ID: paneID, Label: LabelPane, Props: map[string]string{"pid": strconv.Itoa(pid)}})
	wg.graph.AddEdge(paneID, fmt.Sprintf("proc://%d", pid), RelExecutes)
}

// Cypher returns the generated Cypher statements.
func (wg *WorkspaceGraph) Cypher() []string {
	return wg.graph.Cypher()
}

// SaveSession persists a workspace snapshot into FalkorDB. The full JSON
// snapshot is stored on a Session node (for exact restore) and the workspace is
// additionally modeled as queryable graph nodes.
func SaveSession(ctx context.Context, c *Client, spec *layout.SessionSpec) error {
	if c == nil {
		return fmt.Errorf("falkordb client is nil")
	}
	if spec == nil {
		return fmt.Errorf("nil session snapshot")
	}
	name := spec.Name
	if name == "" {
		name = "default"
	}

	payload, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("encoding session: %w", err)
	}

	wg := NewWorkspaceGraph()
	wg.AddSession(spec)

	// Store the JSON snapshot on the session node itself, then write the graph.
	stmts := []string{fmt.Sprintf(
		`MERGE (s:Session {name: "%s"}) SET s.snapshot = "%s"`,
		cypherString(name), cypherString(string(payload)),
	)}
	stmts = append(stmts, wg.Cypher()...)

	for i, stmt := range stmts {
		if _, err := c.Query(ctx, stmt); err != nil {
			return fmt.Errorf("session save statement %d/%d: %w", i+1, len(stmts), err)
		}
	}
	return nil
}

// RestoreSession loads a named session snapshot from FalkorDB. It returns an
// error when the session does not exist.
func RestoreSession(ctx context.Context, c *Client, name string) (*layout.SessionSpec, error) {
	if c == nil {
		return nil, fmt.Errorf("falkordb client is nil")
	}
	if name == "" {
		name = "default"
	}
	query := fmt.Sprintf(`MATCH (s:Session {name: "%s"}) RETURN s.snapshot`, cypherString(name))
	res, err := c.Run(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to restore session: %w", err)
	}
	if len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
		return nil, fmt.Errorf("session %q not found", name)
	}
	raw, _ := res.Rows[0][0].(string)
	if raw == "" {
		return nil, fmt.Errorf("session %q has no snapshot", name)
	}
	var spec layout.SessionSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return nil, fmt.Errorf("session %q is corrupt: %w", name, err)
	}
	return &spec, nil
}

// ListSessions returns the names of every saved session.
func ListSessions(ctx context.Context, c *Client) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("falkordb client is nil")
	}
	res, err := c.Run(ctx, `MATCH (s:Session) RETURN s.name ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, row := range res.Rows {
		if len(row) > 0 {
			if s, ok := row[0].(string); ok {
				names = append(names, s)
			}
		}
	}
	return names, nil
}
