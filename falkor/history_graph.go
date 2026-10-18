package falkor

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// Semantic terminal history: executed shell commands, their exit codes, output
// error summaries and the files they touched are stored as a directed graph so
// success patterns and troubleshooting paths can be queried with Cypher.
//
// Schema:
//
//	(Command {id, command, exit_code, summary, cwd, timestamp})
//	(Command)-[:AFFECTED]->(File {path})
//	(Command)-[:EXECUTED]->(Process {pid})
//	(Command)-[:PRODUCES]->(Output {text})   // only for failures
type CommandHistory struct {
	graph *Graph
}

// NewCommandHistory returns an empty history builder.
func NewCommandHistory() *CommandHistory {
	return &CommandHistory{graph: NewGraph()}
}

// Add records a single command execution. cmdID identifies the run uniquely
// (callers usually pass a session/pane id plus a counter); when empty a
// deterministic id is derived from the command text and timestamp.
func (h *CommandHistory) Add(cmdID, cmdStr string, exitCode int, summary, cwd string, pid int, affectedFiles []string) string {
	if cmdID == "" {
		cmdID = stableSlug(cmdStr)
	}
	id := "cmd://" + cmdID
	props := map[string]string{
		"id":        cmdID,
		"command":   cmdStr,
		"exit_code": strconv.Itoa(exitCode),
		"summary":   summary,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	if cwd != "" {
		props["cwd"] = cwd
	}
	if exitCode == 0 {
		props["status"] = "ok"
	} else {
		props["status"] = "failed"
	}
	h.graph.AddNode(Node{ID: id, Label: LabelCommand, Props: props})

	for _, f := range affectedFiles {
		if f == "" {
			continue
		}
		fid := fileRefNodeID(f)
		h.graph.AddNode(Node{ID: fid, Label: LabelFile, Props: map[string]string{"path": f}})
		h.graph.AddEdge(id, fid, RelAffected)
	}
	if pid > 0 {
		procID := fmt.Sprintf("proc://%d", pid)
		h.graph.AddNode(Node{ID: procID, Label: LabelProcess, Props: map[string]string{"pid": strconv.Itoa(pid)}})
		h.graph.AddEdge(id, procID, RelExecuted)
	}
	if exitCode != 0 && summary != "" {
		outID := id + "/output"
		h.graph.AddNode(Node{ID: outID, Label: LabelOutput, Props: map[string]string{"text": summary}})
		h.graph.AddEdge(id, outID, RelProduces)
	}
	return id
}

// Cypher returns the idempotent write statements for the accumulated history.
func (h *CommandHistory) Cypher() []string { return h.graph.Cypher() }

// RecordCommand stores one executed command (and what it touched) in FalkorDB.
func RecordCommand(ctx context.Context, c *Client, cmdStr string, exitCode int, outputSummary string, affectedFiles []string) error {
	if c == nil {
		return fmt.Errorf("falkordb client is nil")
	}
	h := NewCommandHistory()
	h.Add("", cmdStr, exitCode, outputSummary, "", 0, affectedFiles)
	return execStatements(ctx, c, h.Cypher(), "record command history")
}

// QueryCommandSuccessPatterns returns command/file pairs for commands that
// succeeded.
func QueryCommandSuccessPatterns(ctx context.Context, c *Client) (interface{}, error) {
	if c == nil {
		return nil, fmt.Errorf("falkordb client is nil")
	}
	query := "MATCH (c:Command {status: \"ok\"})-[:AFFECTED]->(f:File) RETURN c.command AS command, f.path AS file"
	return c.Query(ctx, query)
}

// QueryFailedCommands returns the most recent failing commands with their error
// summaries.
func QueryFailedCommands(ctx context.Context, c *Client) (interface{}, error) {
	if c == nil {
		return nil, fmt.Errorf("falkordb client is nil")
	}
	query := "MATCH (c:Command {status: \"failed\"}) RETURN c.command AS command, c.exit_code AS exit_code, c.summary AS summary ORDER BY c.timestamp DESC LIMIT 25"
	return c.Query(ctx, query)
}

// QueryTroubleshootingPaths finds files that fail with some commands but
// succeed with others, which is where a fix was likely discovered.
func QueryTroubleshootingPaths(ctx context.Context, c *Client) (interface{}, error) {
	if c == nil {
		return nil, fmt.Errorf("falkordb client is nil")
	}
	query := "MATCH (fail:Command {status: \"failed\"})-[:AFFECTED]->(f:File)<-[:AFFECTED]-(ok:Command {status: \"ok\"}) " +
		"WHERE fail.command <> ok.command " +
		"RETURN f.path AS file, fail.command AS failing, ok.command AS fixing LIMIT 50"
	return c.Query(ctx, query)
}
