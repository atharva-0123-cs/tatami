package falkor

import (
	"context"
	"fmt"
	"strings"
)

// QueryResult is a parsed FalkorDB response. Columns and Rows are best-effort
// (the RESP shape varies across FalkorDB versions); Raw always holds the
// untouched reply so callers can format it generically.
type QueryResult struct {
	Columns []string
	Rows    [][]interface{}
	Raw     interface{}
}

// Run executes a Cypher statement and returns a parsed result. It rejects
// empty queries so the TUI prompt cannot accidentally run a no-op.
func (c *Client) Run(ctx context.Context, cypher string) (*QueryResult, error) {
	if strings.TrimSpace(cypher) == "" {
		return nil, fmt.Errorf("empty query")
	}
	raw, err := c.Query(ctx, cypher)
	if err != nil {
		return nil, err
	}
	return ParseQueryResult(raw), nil
}

// ParseQueryResult converts the raw GRAPH.QUERY reply into columns and rows.
//
// FalkorDB replies with [header, rows, statistics] where header is a list of
// [type, name] column descriptors. Any other shape is left to Raw/FormatResult.
func ParseQueryResult(raw interface{}) *QueryResult {
	qr := &QueryResult{Raw: raw}
	arr, ok := raw.([]interface{})
	if !ok || len(arr) < 2 {
		return qr
	}
	if header, ok := arr[0].([]interface{}); ok {
		for _, h := range header {
			if col, ok := h.([]interface{}); ok && len(col) >= 2 {
				qr.Columns = append(qr.Columns, fmt.Sprintf("%v", col[len(col)-1]))
			} else {
				qr.Columns = append(qr.Columns, fmt.Sprintf("%v", h))
			}
		}
	}
	if rows, ok := arr[1].([]interface{}); ok {
		for _, r := range rows {
			if row, ok := r.([]interface{}); ok {
				qr.Rows = append(qr.Rows, row)
			}
		}
	}
	return qr
}

// Summarize returns a one-line description of a result, suitable for the
// status bar (for example "3 cols, 12 rows").
func (r *QueryResult) Summarize() string {
	if r == nil {
		return "no result"
	}
	if len(r.Rows) == 0 && len(r.Columns) == 0 {
		return trimOneLine(fmt.Sprintf("%v", r.Raw), 120)
	}
	return fmt.Sprintf("%d cols, %d rows", len(r.Columns), len(r.Rows))
}

// FormatResult renders a raw result as an aligned, human-readable table when
// columns are available, falling back to a generic rendering otherwise.
func FormatResult(raw interface{}) string {
	qr := ParseQueryResult(raw)
	return qr.Format()
}

// Format renders the result, one row per line, columns separated by " | ".
func (r *QueryResult) Format() string {
	if r == nil {
		return ""
	}
	if len(r.Columns) == 0 && len(r.Rows) == 0 {
		return formatGeneric(r.Raw, 0)
	}
	var b strings.Builder
	if len(r.Columns) > 0 {
		b.WriteString(strings.Join(r.Columns, " | "))
		b.WriteString("\n")
	}
	for _, row := range r.Rows {
		cells := make([]string, len(row))
		for i, c := range row {
			cells[i] = formatCell(c)
		}
		b.WriteString(strings.Join(cells, " | "))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatCell renders a single cell compactly.
func formatCell(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []interface{}:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = formatCell(x)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatGeneric renders an arbitrary nested reply, one element per line, with
// two-space indentation per depth level.
func formatGeneric(v interface{}, depth int) string {
	pad := strings.Repeat("  ", depth)
	switch t := v.(type) {
	case []interface{}:
		if len(t) == 0 {
			return pad + "[]"
		}
		var b strings.Builder
		for _, item := range t {
			b.WriteString(formatGeneric(item, depth+1))
			b.WriteString("\n")
		}
		return strings.TrimRight(b.String(), "\n")
	default:
		return pad + fmt.Sprintf("%v", v)
	}
}

// trimOneLine collapses whitespace and truncates a string for status display.
func trimOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}
