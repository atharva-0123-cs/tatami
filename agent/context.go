package agent

import (
	"sort"
	"strings"
)

// ContextSection is one labelled block of agent context. Priority orders the
// blocks: lower values are kept first when the budget is tight.
type ContextSection struct {
	Title    string
	Text     string
	Priority int
}

// ContextProvider is anything that can contribute sections to an agent context
// (a pane, the Git monitor, a command history, ...). This is the agent context
// protocol: providers never assemble text themselves, they only expose sections
// and the Context decides what fits.
type ContextProvider interface {
	AgentSections() []ContextSection
}

// DefaultContextChars is the size budget applied when none is configured.
const DefaultContextChars = 6000

// Context accumulates sections under a character budget and renders them for a
// model. It drops or truncates low-priority sections first so recent, relevant
// terminal output can never crowd out the project instructions.
type Context struct {
	MaxChars int
	sections []ContextSection
}

// NewContext creates an empty context with a character budget (0 uses the
// default).
func NewContext(maxChars int) *Context {
	if maxChars <= 0 {
		maxChars = DefaultContextChars
	}
	return &Context{MaxChars: maxChars}
}

// Add appends a section. Empty titles or bodies are ignored.
func (c *Context) Add(title, text string, priority int) {
	if title == "" || strings.TrimSpace(text) == "" {
		return
	}
	c.sections = append(c.sections, ContextSection{Title: title, Text: text, Priority: priority})
}

// AddProvider adds every section a provider contributes.
func (c *Context) AddProvider(p ContextProvider) {
	if p == nil {
		return
	}
	for _, s := range p.AgentSections() {
		c.Add(s.Title, s.Text, s.Priority)
	}
}

// Render assembles the sections in priority order, truncating the last block
// that does not fit and omitting the rest. A notice is appended when anything
// was dropped so the model knows context was elided.
func (c *Context) Render() string {
	ordered := append([]ContextSection(nil), c.sections...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority < ordered[j].Priority
		}
		return false
	})

	var b strings.Builder
	used := 0
	dropped := 0
	for _, s := range ordered {
		block := "## " + s.Title + "\n" + strings.TrimRight(s.Text, "\n") + "\n\n"
		remaining := c.MaxChars - used
		if remaining <= 0 {
			dropped++
			continue
		}
		if len(block) > remaining {
			block = truncate(block, remaining)
			b.WriteString(block)
			b.WriteString("\n[...truncated...]\n")
			used = c.MaxChars
			dropped++
			continue
		}
		b.WriteString(block)
		used += len(block)
	}
	if dropped > 0 {
		b.WriteString("[" + itoa(dropped) + " section(s) omitted to fit the context budget]\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// FilterOutput condenses terminal output for a model: it strips blank runs and
// keeps the first and last head/tail lines of an over-long block, replacing the
// middle with a marker. This keeps the start of a command's output (where
// errors usually are) and its final result without shipping megabytes.
func FilterOutput(text string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 200
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	// Collapse runs of blank lines.
	compacted := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		compacted = append(compacted, l)
	}
	if len(compacted) <= maxLines {
		return strings.Join(compacted, "\n")
	}
	head := maxLines / 2
	tail := maxLines - head - 1
	out := make([]string, 0, maxLines)
	out = append(out, compacted[:head]...)
	out = append(out, "[... "+itoa(len(compacted)-head-tail)+" lines omitted ...]")
	out = append(out, compacted[len(compacted)-tail:]...)
	return strings.Join(out, "\n")
}

// truncate cuts s to at most n bytes on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8ValidRuneStart(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// utf8ValidRuneStart reports whether b is not a UTF-8 continuation byte.
func utf8ValidRuneStart(b byte) bool { return b&0xC0 != 0x80 }
