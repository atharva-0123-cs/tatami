package falkor

import (
	"fmt"
	"strings"
)

// builtinQueries are named, ready-made traversals exposed by the TUI Cypher
// prompt as ":<name>". They are the "canned" answers to the common dependency
// and call-chain questions; anything else can be typed as raw Cypher.
var builtinQueries = map[string]string{
	"files":     `MATCH (f:File) RETURN f.path AS path, f.language AS language ORDER BY path`,
	"packages":  `MATCH (p:Package) RETURN p.name AS package ORDER BY package`,
	"imports":   `MATCH (f:File)-[:IMPORTS]->(i:Import) RETURN f.path AS file, i.path AS imports ORDER BY file, imports`,
	"structs":   `MATCH (s:Struct) RETURN s.file AS file, s.name AS name, s.kind AS kind ORDER BY file, name`,
	"functions": `MATCH (fn:Function) RETURN fn.file AS file, fn.line AS line, fn.name AS name ORDER BY file, line`,
	"callers":   `MATCH (fn:Function)<-[:CALLS]-(caller:Function) RETURN fn.name AS function, count(caller) AS callers ORDER BY callers DESC LIMIT 25`,
	"calls":     `MATCH (fn:Function)-[:CALLS]->(callee:Function) RETURN fn.name AS caller, callee.name AS callee ORDER BY caller LIMIT 100`,
	"chain":     `MATCH p=(a:Function)-[:CALLS*1..4]->(b:Function) RETURN a.name AS from, b.name AS to, length(p) AS depth ORDER BY depth LIMIT 100`,

	// Multi-agent shared memory.
	"agents":   `MATCH (a:Agent) RETURN a.id AS agent, a.role AS role ORDER BY agent`,
	"findings": `MATCH (a:Agent)-[:FOUND]->(f:Finding)-[:REPORTS]->(b:Bug) RETURN a.id AS agent, f.text AS finding, b.severity AS severity ORDER BY agent`,
	"bugs":     `MATCH (b:Bug) WHERE b.status = "open" OPTIONAL MATCH (b)-[:AFFECTS]->(file:File) RETURN b.id AS bug, b.severity AS severity, b.description AS description, file.path AS file ORDER BY severity`,
	"tasks":    `MATCH (t:Task) OPTIONAL MATCH (a:Agent)-[:ASSIGNED_TO]->(t) RETURN t.id AS task, t.status AS status, a.id AS owner ORDER BY task`,

	// Semantic terminal history.
	"history":  `MATCH (c:Command) RETURN c.command AS command, c.exit_code AS exit_code, c.status AS status ORDER BY c.timestamp DESC LIMIT 50`,
	"failures": `MATCH (c:Command {status: "failed"}) RETURN c.command AS command, c.exit_code AS exit_code, c.summary AS summary ORDER BY c.timestamp DESC LIMIT 25`,
	"fixes":    `MATCH (fail:Command {status: "failed"})-[:AFFECTED]->(f:File)<-[:AFFECTED]-(ok:Command {status: "ok"}) WHERE fail.command <> ok.command RETURN f.path AS file, fail.command AS failing, ok.command AS fixing LIMIT 50`,

	// Workspace/process graph.
	"sessions": `MATCH (s:Session) RETURN s.name AS session ORDER BY session`,
	"panes":    `MATCH (w:Window)-[*]->(p:Pane) RETURN w.name AS window, p.cwd AS cwd, p.pid AS pid ORDER BY window`,
	"procs":    `MATCH (parent:Process)-[:CHILD_PROCESS]->(child:Process) RETURN parent.pid AS parent, child.pid AS child, child.command AS command ORDER BY parent`,
}

// BuiltinNames returns the available ":name" shortcuts, sorted.
func BuiltinNames() []string {
	out := make([]string, 0, len(builtinQueries)+1)
	for name := range builtinQueries {
		out = append(out, name)
	}
	out = append(out, "deps") // argument-aware, not in the map
	sortStrings(out)
	return out
}

// ResolveBuiltin translates a TUI prompt line into a Cypher statement. Lines
// not beginning with ":" are returned unchanged as raw Cypher. It returns false
// only when a ":" line names an unknown command or omits a required argument.
func ResolveBuiltin(input string) (string, bool) {
	body := strings.TrimSpace(input)
	if !strings.HasPrefix(body, ":") {
		return strings.TrimSpace(input), strings.TrimSpace(input) != ""
	}
	body = strings.TrimSpace(body[1:])
	if body == "" {
		return "", false
	}
	fields := strings.Fields(body)
	name, arg := fields[0], strings.Join(fields[1:], " ")

	// Argument-aware builtins take precedence when an argument is supplied.
	if arg != "" {
		if q, ok := argQuery(name, arg); ok {
			return q, true
		}
	}
	if q, ok := builtinQueries[name]; ok {
		return q, true
	}
	return "", false
}

// argQuery builds the parameterized traversals; it returns false for unknown
// names or missing arguments.
func argQuery(name, arg string) (string, bool) {
	switch name {
	case "deps":
		return fmt.Sprintf(
			`MATCH (f:File)-[:IMPORTS]->(i:Import) WHERE f.path CONTAINS %s RETURN f.path AS file, i.path AS imports ORDER BY imports`,
			cypherLiteral(arg),
		), true
	case "callers":
		return fmt.Sprintf(
			`MATCH (fn:Function)<-[:CALLS]-(caller:Function) WHERE fn.name = %s RETURN caller.file AS file, caller.name AS caller ORDER BY file`,
			cypherLiteral(arg),
		), true
	case "chain":
		return fmt.Sprintf(
			`MATCH p=(a:Function {name: %s})-[:CALLS*1..5]->(b:Function) RETURN b.file AS file, b.name AS callee, length(p) AS depth ORDER BY depth LIMIT 100`,
			cypherLiteral(arg),
		), true
	}
	return "", false
}

// cypherLiteral quotes and escapes a user-supplied string for Cypher.
func cypherLiteral(s string) string { return `"` + cypherString(s) + `"` }

// sortStrings is a tiny insertion sort to avoid pulling sort into this file's
// import set for a handful of names.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
