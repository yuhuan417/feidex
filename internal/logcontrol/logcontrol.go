package logcontrol

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

var runtimeLevel slog.LevelVar
var recentLogBuffer = newLogBuffer(200)

type captureHandler struct {
	next   slog.Handler
	attrs  []slog.Attr
	groups []string
}

type logBuffer struct {
	mu      sync.Mutex
	entries []string
	max     int
}

func init() {
	runtimeLevel.Set(slog.LevelInfo)
}

func newLogBuffer(max int) *logBuffer {
	if max <= 0 {
		max = 1
	}
	return &logBuffer{max: max}
}

func LevelVar() *slog.LevelVar {
	return &runtimeLevel
}

func NewHandler(base slog.Handler) slog.Handler {
	if base == nil {
		return nil
	}
	return &captureHandler{next: base}
}

func Set(level slog.Level) {
	runtimeLevel.Set(level)
}

func SetName(name string) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		runtimeLevel.Set(slog.LevelDebug)
	case "", "info":
		runtimeLevel.Set(slog.LevelInfo)
	case "warn", "warning":
		runtimeLevel.Set(slog.LevelWarn)
	case "error":
		runtimeLevel.Set(slog.LevelError)
	default:
		return fmt.Errorf("unsupported slog level %q", name)
	}
	return nil
}

func SetDebug(enabled bool) string {
	if enabled {
		runtimeLevel.Set(slog.LevelDebug)
		return "debug"
	}
	runtimeLevel.Set(slog.LevelInfo)
	return "info"
}

func DebugEnabled() bool {
	return runtimeLevel.Level() <= slog.LevelDebug
}

func CurrentName() string {
	switch level := runtimeLevel.Level(); {
	case level <= slog.LevelDebug:
		return "debug"
	case level <= slog.LevelInfo:
		return "info"
	case level <= slog.LevelWarn:
		return "warn"
	default:
		return "error"
	}
}

func ToggleArgEnabled(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "debug":
		return true, true
	case "off", "info":
		return false, true
	default:
		return false, false
	}
}

func RecentLines(limit int) []string {
	return recentLogBuffer.recent(limit)
}

func (h *captureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h == nil || h.next == nil {
		return false
	}
	return h.next.Enabled(ctx, level)
}

func (h *captureHandler) Handle(ctx context.Context, rec slog.Record) error {
	if h == nil || h.next == nil {
		return nil
	}
	cloned := rec.Clone()
	err := h.next.Handle(ctx, rec)
	recentLogBuffer.append(formatRecord(cloned, h.attrs, h.groups))
	return err
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if h == nil || h.next == nil {
		return h
	}
	nextAttrs := append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &captureHandler{
		next:   h.next.WithAttrs(attrs),
		attrs:  nextAttrs,
		groups: append([]string(nil), h.groups...),
	}
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	if h == nil || h.next == nil {
		return h
	}
	nextGroups := append(append([]string(nil), h.groups...), strings.TrimSpace(name))
	return &captureHandler{
		next:   h.next.WithGroup(name),
		attrs:  append([]slog.Attr(nil), h.attrs...),
		groups: nextGroups,
	}
}

func (b *logBuffer) append(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = append(b.entries, line)
	if len(b.entries) > b.max {
		b.entries = append([]string(nil), b.entries[len(b.entries)-b.max:]...)
	}
}

func (b *logBuffer) recent(limit int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit <= 0 || limit > len(b.entries) {
		limit = len(b.entries)
	}
	if limit == 0 {
		return nil
	}
	start := len(b.entries) - limit
	return append([]string(nil), b.entries[start:]...)
}

func formatRecord(rec slog.Record, attrs []slog.Attr, groups []string) string {
	parts := []string{}
	ts := rec.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	parts = append(parts, ts.Format(time.RFC3339))
	parts = append(parts, strings.ToUpper(rec.Level.String()))
	if msg := strings.TrimSpace(rec.Message); msg != "" {
		parts = append(parts, msg)
	}
	attrParts := make([]string, 0, len(attrs)+4)
	collectAttrStrings(&attrParts, groups, attrs)
	recordAttrs := make([]slog.Attr, 0, rec.NumAttrs())
	rec.Attrs(func(attr slog.Attr) bool {
		recordAttrs = append(recordAttrs, attr)
		return true
	})
	collectAttrStrings(&attrParts, groups, recordAttrs)
	if len(attrParts) > 0 {
		parts = append(parts, strings.Join(attrParts, " "))
	}
	return strings.Join(parts, " ")
}

func collectAttrStrings(out *[]string, prefix []string, attrs []slog.Attr) {
	for _, attr := range attrs {
		attr.Value = attr.Value.Resolve()
		key := strings.TrimSpace(attr.Key)
		if key == "" && attr.Value.Kind() != slog.KindGroup {
			continue
		}
		if attr.Value.Kind() == slog.KindGroup {
			nextPrefix := append([]string(nil), prefix...)
			if key != "" {
				nextPrefix = append(nextPrefix, key)
			}
			collectAttrStrings(out, nextPrefix, attr.Value.Group())
			continue
		}
		fullKey := key
		if len(prefix) > 0 {
			fullKey = strings.Join(append(append([]string(nil), prefix...), key), ".")
		}
		*out = append(*out, fullKey+"="+formatValue(attr.Value))
	}
}

func formatValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		return value.String()
	default:
		return fmt.Sprint(value.Any())
	}
}
