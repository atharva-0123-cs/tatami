package falkor

import "sort"

// SchemaTokens lists every distinct node label and relationship type in the
// built-in graph schema (code graph plus the workspace, agent and history
// graphs), sorted for stable display. The TUI uses it to offer completion of
// labels and relationship types while typing Cypher.
func SchemaTokens() []string {
	raw := []string{
		// Code graph labels.
		LabelProject, LabelPackage, LabelFile, LabelImport, LabelFunction,
		LabelStruct, LabelField, LabelVariable,
		// Workspace/agent/history labels.
		LabelWindow, LabelNode, LabelPane, LabelDirectory, LabelProcess,
		LabelAgent, LabelBug, LabelFinding, LabelTask, LabelCommand,
		LabelOutput, LabelSession,
		// Code graph relationships.
		RelBelongsTo, RelContains, RelImports, RelCalls, RelHasField, RelDependsOn,
		// Workspace/agent/history relationships.
		RelRoot, RelChild, RelContent, RelWorkingIn, RelChildProcess, RelExecutes,
		RelFound, RelAffects, RelExecuted, RelAffected, RelAssigned, RelBlocks,
		RelReports, RelProduces,
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
