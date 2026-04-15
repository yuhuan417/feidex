package logcontrol

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type stubHandler struct {
	enabled     bool
	handleErr   error
	handleCalls int
	lastRecord  slog.Record
	attrs       []slog.Attr
	groups      []string
}

func (h *stubHandler) Enabled(context.Context, slog.Level) bool {
	return h != nil && h.enabled
}

func (h *stubHandler) Handle(_ context.Context, rec slog.Record) error {
	h.handleCalls++
	h.lastRecord = rec.Clone()
	return h.handleErr
}

func (h *stubHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cp := *h
	cp.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &cp
}

func (h *stubHandler) WithGroup(name string) slog.Handler {
	cp := *h
	cp.groups = append(append([]string(nil), h.groups...), strings.TrimSpace(name))
	return &cp
}

func resetLogcontrolForTest(t *testing.T) {
	t.Helper()
	prevLevel := runtimeLevel.Level()
	prevBuffer := recentLogBuffer
	runtimeLevel.Set(slog.LevelInfo)
	recentLogBuffer = newLogBuffer(32)
	t.Cleanup(func() {
		runtimeLevel.Set(prevLevel)
		recentLogBuffer = prevBuffer
	})
}

func TestLevelAndToggleHelpers(t *testing.T) {
	resetLogcontrolForTest(t)

	if LevelVar() != &runtimeLevel {
		t.Fatal("LevelVar() should return the runtime level var")
	}
	if NewHandler(nil) != nil {
		t.Fatal("NewHandler(nil) should return nil")
	}

	Set(slog.LevelWarn)
	if got := CurrentName(); got != "warn" {
		t.Fatalf("CurrentName() after Set(warn) = %q", got)
	}

	cases := []struct {
		name  string
		level string
		want  string
	}{
		{name: "debug", level: "debug", want: "debug"},
		{name: "info", level: " info ", want: "info"},
		{name: "warning", level: "warning", want: "warn"},
		{name: "error", level: "error", want: "error"},
	}
	for _, tc := range cases {
		if err := SetName(tc.level); err != nil {
			t.Fatalf("SetName(%s) error = %v", tc.name, err)
		}
		if got := CurrentName(); got != tc.want {
			t.Fatalf("CurrentName() after %s = %q, want %q", tc.name, got, tc.want)
		}
	}
	if err := SetName("trace"); err == nil {
		t.Fatal("SetName(trace) should fail")
	}

	if got := SetDebug(true); got != "debug" || !DebugEnabled() {
		t.Fatalf("SetDebug(true) = %q, debug=%v", got, DebugEnabled())
	}
	if got := SetDebug(false); got != "info" || DebugEnabled() {
		t.Fatalf("SetDebug(false) = %q, debug=%v", got, DebugEnabled())
	}

	toggleCases := []struct {
		arg    string
		want   bool
		wantOK bool
	}{
		{arg: "on", want: true, wantOK: true},
		{arg: "debug", want: true, wantOK: true},
		{arg: "off", want: false, wantOK: true},
		{arg: " info ", want: false, wantOK: true},
		{arg: "other", want: false, wantOK: false},
	}
	for _, tc := range toggleCases {
		got, ok := ToggleArgEnabled(tc.arg)
		if got != tc.want || ok != tc.wantOK {
			t.Fatalf("ToggleArgEnabled(%q) = %v, %v; want %v, %v", tc.arg, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestLogBufferRecentAndTrim(t *testing.T) {
	b := newLogBuffer(0)
	if b.max != 1 {
		t.Fatalf("newLogBuffer(0).max = %d, want 1", b.max)
	}
	b.append("")
	b.append("  ")
	b.append("first")
	b.append("second")
	if got := b.recent(5); len(got) != 1 || got[0] != "second" {
		t.Fatalf("recent on max=1 buffer = %#v", got)
	}

	b = newLogBuffer(2)
	b.append("one")
	b.append("two")
	b.append("three")
	if got := b.recent(0); len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Fatalf("recent(0) = %#v, want [two three]", got)
	}
	if got := b.recent(1); len(got) != 1 || got[0] != "three" {
		t.Fatalf("recent(1) = %#v, want [three]", got)
	}
}

func TestCaptureHandlerDelegatesAndCapturesRecentLogs(t *testing.T) {
	resetLogcontrolForTest(t)

	base := &stubHandler{enabled: true}
	handler, _ := NewHandler(base).(*captureHandler)
	if handler == nil {
		t.Fatal("NewHandler(base) should wrap the base handler")
	}
	if !handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Enabled() should delegate to the base handler")
	}

	nilHandler := (*captureHandler)(nil)
	if nilHandler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("nil handler should be disabled")
	}
	if err := nilHandler.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "ignored", 0)); err != nil {
		t.Fatalf("nil handler Handle() error = %v", err)
	}

	handler, _ = handler.WithAttrs([]slog.Attr{
		slog.String("request_id", "req-1"),
		slog.Group("nested", slog.String("key", "value")),
	}).(*captureHandler)
	handler, _ = handler.WithGroup("scope").(*captureHandler)

	rec := slog.NewRecord(time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC), slog.LevelWarn, "sync failed", 0)
	rec.AddAttrs(slog.Int("count", 3), slog.Group("ctx", slog.String("user", "ou_1")))
	if err := handler.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	nextHandler, _ := handler.next.(*stubHandler)
	if nextHandler == nil || nextHandler.handleCalls != 1 {
		t.Fatalf("wrapped handle calls = %+v, want 1", nextHandler)
	}
	lines := RecentLines(10)
	if len(lines) != 1 {
		t.Fatalf("RecentLines() = %#v, want 1 line", lines)
	}
	line := lines[0]
	for _, want := range []string{
		"2026-04-15T12:00:00Z",
		"WARN",
		"sync failed",
		"scope.request_id=req-1",
		"scope.nested.key=value",
		"scope.count=3",
		"scope.ctx.user=ou_1",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("captured log line missing %q: %q", want, line)
		}
	}
}

func TestCaptureHandlerPropagatesErrorAndFormatRecordHandlesGroups(t *testing.T) {
	resetLogcontrolForTest(t)

	base := &stubHandler{enabled: true, handleErr: errors.New("boom")}
	handler, _ := NewHandler(base).(*captureHandler)
	rec := slog.NewRecord(time.Now(), slog.LevelError, "write failed", 0)
	if err := handler.Handle(context.Background(), rec); err == nil || err.Error() != "boom" {
		t.Fatalf("Handle() error = %v, want boom", err)
	}
	if lines := RecentLines(10); len(lines) != 1 || !strings.Contains(lines[0], "write failed") {
		t.Fatalf("RecentLines() after error = %#v", lines)
	}

	grouped := slog.NewRecord(time.Time{}, slog.LevelInfo, "", 0)
	grouped.AddAttrs(
		slog.Bool("ok", true),
		slog.Group("meta", slog.Int("count", 2)),
	)
	line := formatRecord(grouped, []slog.Attr{
		slog.Group("", slog.String("inner", "value")),
	}, []string{"outer"})
	if !strings.Contains(line, "INFO") || !strings.Contains(line, "outer.inner=value") || !strings.Contains(line, "outer.ok=true") || !strings.Contains(line, "outer.meta.count=2") {
		t.Fatalf("formatRecord() = %q", line)
	}
}
