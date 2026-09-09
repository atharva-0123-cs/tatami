// Package format implements the human-readable log handler. Every record is
// rendered as a single line in the layout
//
//	[INFO] 2026-09-08T19:26:59.650+05:30 main.go:199 : tatami exit uptime=1.2s
//
// The level tag is one of DEBUG, INFO, WARN or ERROR (finer/custom levels map
// to the nearest visible tag), followed by the wall-clock time, the source
// file and line of the log call, the message, and any key=value attributes.
package format

import (
	"context"
	"encoding"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ConsoleHandler is a slog.Handler that writes leveled, time-stamped records
// to an io.Writer. It is safe for concurrent use.
type ConsoleHandler struct {
	mu     *sync.Mutex
	w      io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

// NewConsoleHandler returns a ConsoleHandler writing to w at the level given
// in opts (or Info when opts is nil). With Source set in opts, each record is
// annotated with the calling file and line.
func NewConsoleHandler(w io.Writer, opts *slog.HandlerOptions) *ConsoleHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &ConsoleHandler{
		mu:    &sync.Mutex{},
		w:     w,
		level: opts.Level,
	}
}

// Enabled reports whether records at the given level should be emitted.
func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle writes one record in the "[LEVEL] time file:line : message" layout.
func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	buf := make([]byte, 0, 256)

	buf = append(buf, '[')
	buf = append(buf, levelName(r.Level)...)
	buf = append(buf, ']', ' ')

	buf = r.Time.AppendFormat(buf, "2006-01-02T15:04:05.000-07:00")
	buf = append(buf, ' ')

	if src := r.Source(); src != nil {
		file := src.File
		if i := strings.LastIndexByte(file, '/'); i >= 0 {
			file = file[i+1:]
		}
		buf = append(buf, file...)
		buf = append(buf, ':')
		buf = strconv.AppendInt(buf, int64(src.Line), 10)
		buf = append(buf, ' ')
	}

	buf = append(buf, ':')
	buf = append(buf, ' ')
	buf = append(buf, r.Message...)

	// Handler-level attributes (slog.Logger.With) come first, then the
	// record's own attributes.
	groups := h.groups
	attrs := h.attrs
	if r.NumAttrs() > 0 {
		r.Attrs(func(a slog.Attr) bool {
			attrs = append(attrs, a)
			return true
		})
	}
	if len(attrs) == 0 {
		buf = append(buf, '\n')
	} else {
		for _, a := range attrs {
			buf = appendAttribute(buf, groups, a)
		}
		buf = append(buf, '\n')
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf)
	return err
}

// WithAttrs returns a copy of h with the given attributes pre-attached to
// every subsequent record.
func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ConsoleHandler{
		mu:     h.mu,
		w:      h.w,
		level:  h.level,
		groups: h.groups,
		attrs:  appendAttrs(h.attrs, attrs),
	}
}

// WithGroup returns a copy of h whose subsequent attributes are grouped under
// the given name (rendered as "group.key=value").
func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &ConsoleHandler{
		mu:     h.mu,
		w:      h.w,
		level:  h.level,
		attrs:  h.attrs,
		groups: append(h.groups, name),
	}
}

// levelName maps a slog level to its visible tag, folding finer/custom levels
// toward the nearest standard tag.
func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

// appendAttribute renders one attribute (and any attributes opened inside
// groups) as " key=value".
func appendAttribute(buf []byte, groups []string, a slog.Attr) []byte {
	if a.Equal(slog.Attr{}) {
		return buf
	}
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		groups = append(groups, a.Key)
		for _, child := range a.Value.Group() {
			buf = appendAttribute(buf, groups, child)
		}
		groups = groups[:len(groups)-1]
		return buf
	}
	if a.Key == "" {
		return buf
	}
	key := a.Key
	if len(groups) > 0 {
		key = strings.Join(append(groups, a.Key), ".")
	}
	buf = append(buf, ' ')
	buf = append(buf, key...)
	buf = append(buf, '=')
	buf = appendValue(buf, a.Value)
	return buf
}

// appendValue renders an attribute value. Strings are quoted when they contain
// whitespace or characters that would break the key=value layout.
func appendValue(buf []byte, v slog.Value) []byte {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if needsQuote(s) {
			return strconv.AppendQuote(buf, s)
		}
		return append(buf, s...)

	case slog.KindInt64:
		return strconv.AppendInt(buf, v.Int64(), 10)

	case slog.KindUint64:
		return strconv.AppendUint(buf, v.Uint64(), 10)

	case slog.KindFloat64:
		return strconv.AppendFloat(buf, v.Float64(), 'g', -1, 64)

	case slog.KindBool:
		return strconv.AppendBool(buf, v.Bool())

	case slog.KindTime:
		return v.Time().AppendFormat(buf, time.RFC3339Nano)

	case slog.KindDuration:
		return append(buf, v.Duration().String()...)

	case slog.KindGroup:
		// Handled in appendAttribute; anonymous groups only reach here when
		// a Value was built directly with KindGroup.
		buf = append(buf, '(')
		for i, a := range v.Group() {
			if i > 0 {
				buf = append(buf, ' ')
			}
			buf = appendAttribute(buf, nil, a)
		}
		return append(buf, ')')

	case slog.KindLogValuer:
		return appendValue(buf, v.Resolve())

	case slog.KindAny:
		if v.Any() == nil {
			return append(buf, "<nil>"...)
		}
		if s, ok := v.Any().(fmt.Stringer); ok {
			return appendValue(buf, slog.StringValue(s.String()))
		}
		if t, ok := v.Any().(encoding.TextMarshaler); ok {
			if b, err := t.MarshalText(); err == nil {
				return appendValue(buf, slog.StringValue(string(b)))
			}
		}
		if err, ok := v.Any().(error); ok {
			return appendValue(buf, slog.StringValue(err.Error()))
		}
		return fmt.Appendf(buf, "%v", v.Any())
	}
	return buf
}

// needsQuote reports whether a string value must be quoted to survive the
// space-separated key=value layout.
func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r <= ' ' || r == '=' || r == '"' || r == '\\' {
			return true
		}
	}
	return false
}

// appendAttrs merges existing and new attribute lists, dropping duplicates in
// favor of the new value.
func appendAttrs(dst, src []slog.Attr) []slog.Attr {
	if len(dst) == 0 {
		return src
	}
	merged := make([]slog.Attr, 0, len(dst)+len(src))
	keys := make(map[string]bool, len(dst)+len(src))
	for _, a := range append(append([]slog.Attr{}, dst...), src...) {
		if a.Equal(slog.Attr{}) {
			continue
		}
		if keys[a.Key] {
			continue
		}
		keys[a.Key] = true
		merged = append(merged, a)
	}
	return merged
}
