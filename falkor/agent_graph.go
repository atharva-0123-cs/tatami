package falkor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Shared memory graph for multi-agent coordination.
//
// Schema:
//
//	(Agent {id, role})-[:FOUND]->(Finding {id, text})
//	(Finding)-[:REPORTS]->(Bug {id, description, severity, status})
//	(Bug)-[:AFFECTS]->(File {path})
//	(Agent)-[:ASSIGNED_TO]->(Task {id, description, status})
//	(Task)-[:DEPENDS_ON]->(Task)
//	(Task)-[:BLOCKS]->(Task)
//
// Agents in different panes write findings, bug reports and task dependencies
// into this graph and read each other's work with Cypher, which is the shared
// blackboard that makes coordination possible without a central coordinator.
type AgentMemory struct {
	graph *Graph
}

// NewAgentMemory returns an empty shared-memory builder.
func NewAgentMemory() *AgentMemory {
	return &AgentMemory{graph: NewGraph()}
}

// agentNodeID is the stable node key for an agent.
func agentNodeID(id string) string { return "agent://" + id }

// fileRefNodeID maps a filesystem path to the same File node the code
// knowledge graph uses: relative to the current working directory when the
// path lives beneath it, otherwise the cleaned absolute path.
func fileRefNodeID(path string) string {
	cleaned := filepath.Clean(path)
	if cwd, err := os.Getwd(); err == nil {
		if rel, rerr := filepath.Rel(cwd, cleaned); rerr == nil && !strings.HasPrefix(rel, "..") {
			return fileNodeID(rel)
		}
	}
	return fileNodeID(cleaned)
}

// AddAgent registers an agent and its role.
func (m *AgentMemory) AddAgent(id, role string) {
	if id == "" {
		return
	}
	m.graph.AddNode(Node{ID: agentNodeID(id), Label: LabelAgent, Props: map[string]string{
		"id": id, "role": role, "name": id,
	}})
}

// AddFinding records a finding produced by an agent and links FOUND.
func (m *AgentMemory) AddFinding(agentID, findingID, text string) {
	if agentID == "" || findingID == "" {
		return
	}
	m.AddAgent(agentID, "")
	fid := "finding://" + findingID
	m.graph.AddNode(Node{ID: fid, Label: LabelFinding, Props: map[string]string{
		"id": findingID, "text": text,
	}})
	m.graph.AddEdge(agentNodeID(agentID), fid, RelFound)
}

// AddBug records a bug report.
func (m *AgentMemory) AddBug(bugID, description, severity, status string) {
	if bugID == "" {
		return
	}
	if status == "" {
		status = "open"
	}
	if severity == "" {
		severity = "normal"
	}
	m.graph.AddNode(Node{ID: "bug://" + bugID, Label: LabelBug, Props: map[string]string{
		"id": bugID, "description": description, "severity": severity, "status": status,
	}})
}

// ReportBug links a finding to the bug it reports.
func (m *AgentMemory) ReportBug(findingID, bugID string) {
	if findingID == "" || bugID == "" {
		return
	}
	m.graph.AddEdge("finding://"+findingID, "bug://"+bugID, RelReports)
}

// AddBugFile links a bug to a file it affects.
func (m *AgentMemory) AddBugFile(bugID, path string) {
	if bugID == "" || path == "" {
		return
	}
	fid := fileRefNodeID(path)
	m.graph.AddNode(Node{ID: fid, Label: LabelFile, Props: map[string]string{"path": filepath.ToSlash(path)}})
	m.graph.AddEdge("bug://"+bugID, fid, RelAffects)
}

// AddTask registers a task in a given status.
func (m *AgentMemory) AddTask(taskID, description, status string) {
	if taskID == "" {
		return
	}
	if status == "" {
		status = "todo"
	}
	m.graph.AddNode(Node{ID: "task://" + taskID, Label: LabelTask, Props: map[string]string{
		"id": taskID, "description": description, "status": status,
	}})
}

// AssignTask links an agent to the task it owns.
func (m *AgentMemory) AssignTask(agentID, taskID string) {
	if agentID == "" || taskID == "" {
		return
	}
	m.AddAgent(agentID, "")
	m.graph.AddEdge(agentNodeID(agentID), "task://"+taskID, RelAssigned)
}

// AddTaskDependency records that task depends on dep (DEPENDS_ON) and the
// inverse BLOCKS edge so either direction can be queried.
func (m *AgentMemory) AddTaskDependency(taskID, dep string) {
	if taskID == "" || dep == "" {
		return
	}
	m.graph.AddEdge("task://"+taskID, "task://"+dep, RelDependsOn)
	m.graph.AddEdge("task://"+dep, "task://"+taskID, RelBlocks)
}

// Cypher returns the idempotent write statements for the accumulated memory.
func (m *AgentMemory) Cypher() []string { return m.graph.Cypher() }

// RecordAgentFinding records an agent finding, the bug it reports, and the file
// the bug affects, then writes them to FalkorDB.
func RecordAgentFinding(ctx context.Context, c *Client, agentID, bugDesc, filePath string) error {
	if c == nil {
		return fmt.Errorf("falkordb client is nil")
	}
	bugID := stableSlug(bugDesc)
	m := NewAgentMemory()
	m.AddAgent(agentID, "coder")
	m.AddFinding(agentID, bugID, bugDesc)
	m.AddBug(bugID, bugDesc, "normal", "open")
	m.ReportBug(bugID, bugID)
	if filePath != "" {
		m.AddBugFile(bugID, filePath)
	}
	return execStatements(ctx, c, m.Cypher(), "record agent finding")
}

// RecordTaskDependency records that task depends on another task.
func RecordTaskDependency(ctx context.Context, c *Client, agentID, taskID, dependsOn string) error {
	if c == nil {
		return fmt.Errorf("falkordb client is nil")
	}
	m := NewAgentMemory()
	if agentID != "" {
		m.AddAgent(agentID, "coder")
		m.AssignTask(agentID, taskID)
	}
	m.AddTask(taskID, "", "todo")
	m.AddTask(dependsOn, "", "todo")
	m.AddTaskDependency(taskID, dependsOn)
	return execStatements(ctx, c, m.Cypher(), "record task dependency")
}

// QueryAgentFindings returns every agent's findings with the bugs they report
// and the files those bugs affect.
func QueryAgentFindings(ctx context.Context, c *Client) (interface{}, error) {
	if c == nil {
		return nil, fmt.Errorf("falkordb client is nil")
	}
	query := "MATCH (a:Agent)-[:FOUND]->(f:Finding)-[:REPORTS]->(b:Bug)-[:AFFECTS]->(file:File) " +
		"RETURN a.id AS agent, f.text AS finding, b.severity AS severity, file.path AS file"
	return c.Query(ctx, query)
}

// QueryOpenBugs returns every open bug with its affected files.
func QueryOpenBugs(ctx context.Context, c *Client) (interface{}, error) {
	if c == nil {
		return nil, fmt.Errorf("falkordb client is nil")
	}
	query := "MATCH (b:Bug {status: \"open\"})-[:AFFECTS]->(f:File) " +
		"RETURN b.id AS bug, b.description AS description, b.severity AS severity, f.path AS file"
	return c.Query(ctx, query)
}

// WaitForFinding blocks until a finding matching the optional agent id exists
// or the context expires. This is the polling synchronization primitive agents
// use to wait for another pane's results without a central broker.
func WaitForFinding(ctx context.Context, c *Client, agentID string, poll time.Duration) error {
	if c == nil {
		return fmt.Errorf("falkordb client is nil")
	}
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	query := "MATCH (a:Agent)-[:FOUND]->(:Finding) RETURN count(a) AS n"
	if agentID != "" {
		query = fmt.Sprintf("MATCH (a:Agent {id: %s})-[:FOUND]->(:Finding) RETURN count(a) AS n", cypherLiteral(agentID))
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, err := c.Run(ctx, query)
		if err == nil && countFromResult(res) > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// countFromResult extracts the first numeric cell of a result, returning 0 when
// absent.
func countFromResult(res *QueryResult) int {
	if res == nil || len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
		return 0
	}
	switch v := res.Rows[0][0].(type) {
	case int64:
		return int(v)
	case int:
		return v
	case string:
		n := 0
		fmt.Sscanf(v, "%d", &n)
		return n
	default:
		return 0
	}
}

// execStatements runs a list of Cypher statements, wrapping the first failure.
func execStatements(ctx context.Context, c *Client, stmts []string, what string) error {
	for i, stmt := range stmts {
		if _, err := c.Query(ctx, stmt); err != nil {
			return fmt.Errorf("%s (statement %d/%d): %w", what, i+1, len(stmts), err)
		}
	}
	return nil
}

// stableSlug derives a deterministic, Cypher-safe id from a description.
func stableSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 48 {
		out = out[:48]
	}
	if out == "" {
		out = "item"
	}
	return out
}
