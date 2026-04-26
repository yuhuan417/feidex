package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuietWorkingCardHelperBranches(t *testing.T) {
	if got := buildQuietWebSearchLines(map[string]any{
		"action": map[string]any{
			"type": "findInPage",
			"url":  " https://example.test/page ",
		},
	}); len(got) != 1 || got[0] != "Find in page: `https://example.test/page`" {
		t.Fatalf("buildQuietWebSearchLines(findInPage) = %#v", got)
	}

	if got := buildQuietWebSearchLines(map[string]any{
		"action": map[string]any{
			"queries": []any{" alpha ", "", "beta"},
		},
	}); len(got) != 1 || got[0] != "Searching the web: `alpha | beta`" {
		t.Fatalf("buildQuietWebSearchLines(queries) = %#v", got)
	}

	if got := buildQuietWebSearchLines(map[string]any{
		"action": map[string]any{"type": "openPage"},
	}); got != nil {
		t.Fatalf("buildQuietWebSearchLines(openPage) = %#v, want nil", got)
	}

	if got := buildQuietCommandExecutionLines(map[string]any{
		"status": "in_progress",
		"commandActions": []any{
			map[string]any{"type": "read", "name": "quiet_mode.go"},
		},
	}, ""); got != nil {
		t.Fatalf("buildQuietCommandExecutionLines(non-completed) = %#v, want nil", got)
	}

	if got := quietDisplayFileName(filepath.Join("internal", "app", "quiet_mode.go") + ":12"); got != "quiet_mode.go" {
		t.Fatalf("quietDisplayFileName() = %q", got)
	}

	if got := joinQuietStringList([]string{" a ", "", "b "}); got != "a | b" {
		t.Fatalf("joinQuietStringList([]string) = %q", got)
	}
	if got := joinQuietStringList([]any{" a ", "", "b "}); got != "a | b" {
		t.Fatalf("joinQuietStringList([]any) = %q", got)
	}

	if got := markdownInlineCode(" `a` "); got != "`'a'`" {
		t.Fatalf("markdownInlineCode() = %q", got)
	}
	if got := normalizeWorkingStatus(" in_progress "); got != "inprogress" {
		t.Fatalf("normalizeWorkingStatus() = %q", got)
	}
	if got := normalizeCommandActionType("list-files"); got != "listfiles" {
		t.Fatalf("normalizeCommandActionType() = %q", got)
	}
	if got := normalizeWebSearchActionType("find_in_page"); got != "findinpage" {
		t.Fatalf("normalizeWebSearchActionType() = %q", got)
	}
	if got := quietPatchChangeType(map[string]any{"kind": "Update"}); got != "update" {
		t.Fatalf("quietPatchChangeType() = %q", got)
	}
	if got := quietPatchMovePath(map[string]any{"movePath": "next.go"}); got != "next.go" {
		t.Fatalf("quietPatchMovePath() = %q", got)
	}

	verb, tail, ok := parseQuietMergeableLine("Read `a.go`")
	if !ok || verb != "Read" || tail != "`a.go`" {
		t.Fatalf("parseQuietMergeableLine(Read) = %q %q %v", verb, tail, ok)
	}
	if _, _, ok := parseQuietMergeableLine("Search `foo`"); ok {
		t.Fatal("parseQuietMergeableLine(Search) should not be mergeable")
	}

	card := &quietWorkingCard{}
	if !card.ReplaceEntries(quietWorkingReasoningKey, []string{"思考中..."}) {
		t.Fatal("ReplaceEntries(reasoning) should report change")
	}
	if !card.IsReasoningOnly() {
		t.Fatal("card should be reasoning-only")
	}
	if got := card.LinesForPrefix(quietWorkingReasoningKey); len(got) != 1 || got[0] != "思考中..." {
		t.Fatalf("LinesForPrefix(reasoning) = %#v", got)
	}
	if !card.ReplaceEntries("item:1", []string{"Read `a.go`", "Read `b.go`", "List `internal/app`"}) {
		t.Fatal("ReplaceEntries(item) should report change")
	}
	body := card.Body()
	if !strings.Contains(body, "Read `a.go` `b.go`") || !strings.Contains(body, "List `internal/app`") {
		t.Fatalf("Body() = %q", body)
	}
	if !card.RemoveEntries(quietWorkingReasoningKey) {
		t.Fatal("RemoveEntries(reasoning) should report change")
	}
	if card.IsReasoningOnly() {
		t.Fatal("card should no longer be reasoning-only")
	}
	if card.RemoveEntries("missing") {
		t.Fatal("RemoveEntries(missing) should not report change")
	}
	if prefix := quietWorkingEntryPrefix(quietWorkingEntryKey("item:2", 3)); prefix != "item:2" {
		t.Fatalf("quietWorkingEntryPrefix() = %q", prefix)
	}
	if equalStringSlices([]string{"a"}, []string{"b"}) || !equalStringSlices([]string{"a"}, []string{"a"}) {
		t.Fatal("equalStringSlices() returned unexpected result")
	}
}

func TestCompactQuietWorkingLinesDeduplicates(t *testing.T) {
	lines := compactQuietWorkingLines([]string{
		"Read `a.go`",
		"Read `a.go`",
		"Read `b.go`",
	})
	if len(lines) != 1 || lines[0] != "Read `a.go` `b.go`" {
		t.Fatalf("dedup same file: lines = %#v", lines)
	}

	lines = compactQuietWorkingLines([]string{
		"Update `x.go`",
		"Update `x.go`",
		"Update `y.go`",
	})
	if len(lines) != 1 || lines[0] != "Update `x.go` `y.go`" {
		t.Fatalf("dedup update: lines = %#v", lines)
	}
}

func TestCompactQuietWorkingLinesDedupWithKeys(t *testing.T) {
	// Same dedup key → deduplicated.
	lines := compactQuietWorkingLinesWithDedup(
		[]string{"Read `a.go`", "Read `a.go`"},
		[]string{"src/a.go", "src/a.go"},
	)
	if len(lines) != 1 || lines[0] != "Read `a.go`" {
		t.Fatalf("same dedup key: lines = %#v", lines)
	}

	// Different dedup keys → not deduplicated even if display names are same.
	lines = compactQuietWorkingLinesWithDedup(
		[]string{"Read `a.go`", "Read `a.go`"},
		[]string{"src/a.go", "test/a.go"},
	)
	if len(lines) != 1 || lines[0] != "Read `a.go` `a.go`" {
		t.Fatalf("diff dedup key same name: lines = %#v", lines)
	}

	// Mixed: some same, some different.
	lines = compactQuietWorkingLinesWithDedup(
		[]string{"Read `a.go`", "Read `a.go`", "Read `b.go`", "Read `a.go`"},
		[]string{"src/a.go", "src/a.go", "src/b.go", "test/a.go"},
	)
	if len(lines) != 1 || lines[0] != "Read `a.go` `b.go` `a.go`" {
		t.Fatalf("mixed dedup: lines = %#v", lines)
	}
}

func TestQuietWorkingCardBodyDedupKeys(t *testing.T) {
	card := &quietWorkingCard{
		Entries: map[string]string{
			"item:t1\x000": "Read `a.go`",
			"item:t1\x001": "Read `a.go`",
			"item:t1\x002": "Read `b.go`",
		},
		EntryOrder: []string{"item:t1\x000", "item:t1\x001", "item:t1\x002"},
		DedupKeys: map[string]string{
			"item:t1\x000": "src/a.go",
			"item:t1\x001": "src/a.go",
			"item:t1\x002": "src/b.go",
		},
	}
	body := card.Body()
	if body != "Read `a.go` `b.go`" {
		t.Fatalf("body with dedup keys = %q", body)
	}

	// Same display name, different dedup keys → both shown.
	card2 := &quietWorkingCard{
		Entries: map[string]string{
			"item:t2\x000": "Read `a.go`",
			"item:t2\x001": "Read `a.go`",
		},
		EntryOrder: []string{"item:t2\x000", "item:t2\x001"},
		DedupKeys: map[string]string{
			"item:t2\x000": "src/a.go",
			"item:t2\x001": "test/a.go",
		},
	}
	body2 := card2.Body()
	if body2 != "Read `a.go` `a.go`" {
		t.Fatalf("body same name diff key = %q", body2)
	}
}

func TestQuietWorkingCardLifecycleBranches(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	workspace := a.cfg.Workspaces[0].Cwd

	if got := prepareQuietWorkingCardUpdateLocked(nil, "noop", map[string]any{"type": "reasoning"}, workspace); got != (quietWorkingCardOp{}) {
		t.Fatalf("prepareQuietWorkingCardUpdateLocked(nil) = %+v", got)
	}

	stream := &turnStream{TurnID: "turn-1"}
	if got := prepareQuietWorkingCardUpdateLocked(stream, "noop", map[string]any{
		"type": "webSearch",
		"action": map[string]any{
			"type": "openPage",
		},
	}, workspace); got != (quietWorkingCardOp{}) {
		t.Fatalf("prepareQuietWorkingCardUpdateLocked(no visible lines) = %+v", got)
	}

	reasoningKey := quietWorkingEntryKey(quietWorkingReasoningKey, 0)
	stream.QuietWorking = &quietWorkingCard{
		MessageID:    "msg-work",
		EntryOrder:   []string{reasoningKey},
		Entries:      map[string]string{reasoningKey: "思考中..."},
		RenderedBody: "思考中...",
	}
	op := prepareQuietWorkingCardUpdateLocked(stream, "cmd-1", map[string]any{
		"type":   "commandExecution",
		"status": "completed",
		"commandActions": []any{
			map[string]any{"type": "read", "name": "quiet_mode.go"},
		},
	}, workspace)
	if op.MessageID != "msg-work" || !strings.Contains(op.Body, "Read `quiet_mode.go`") || strings.Contains(op.Body, "思考中...") {
		t.Fatalf("prepareQuietWorkingCardUpdateLocked(command) = %+v", op)
	}
	if got := prepareQuietWorkingCardUpdateLocked(stream, "cmd-1", map[string]any{
		"type":   "commandExecution",
		"status": "completed",
		"commandActions": []any{
			map[string]any{"type": "read", "name": "quiet_mode.go"},
		},
	}, workspace); got != (quietWorkingCardOp{}) {
		t.Fatalf("prepareQuietWorkingCardUpdateLocked(unchanged) = %+v", got)
	}

	reasoningOnly := &turnStream{
		QuietWorking: &quietWorkingCard{
			MessageID:  "reuse-1",
			EntryOrder: []string{reasoningKey},
			Entries:    map[string]string{reasoningKey: "思考中..."},
		},
	}
	boundary := prepareQuietWorkingCardBoundaryLocked(reasoningOnly)
	if boundary.ReuseMessageID != "reuse-1" || boundary.Op != (quietWorkingCardOp{}) || reasoningOnly.QuietWorking != nil {
		t.Fatalf("prepareQuietWorkingCardBoundaryLocked(reasoning-only) = %+v, stream=%+v", boundary, reasoningOnly)
	}

	itemKey := quietWorkingEntryKey("item:cmd-1", 0)
	mixed := &turnStream{
		TurnID: "turn-2",
		QuietWorking: &quietWorkingCard{
			MessageID:    "patch-1",
			RenderedBody: "思考中...\nRead `quiet_mode.go`",
			EntryOrder:   []string{reasoningKey, itemKey},
			Entries: map[string]string{
				reasoningKey: "思考中...",
				itemKey:      "Read `quiet_mode.go`",
			},
		},
	}
	boundary = prepareQuietWorkingCardBoundaryLocked(mixed)
	if boundary.ReuseMessageID != "" || boundary.Op.MessageID != "patch-1" || boundary.Op.Body != "Read `quiet_mode.go`" || mixed.QuietWorking != nil {
		t.Fatalf("prepareQuietWorkingCardBoundaryLocked(mixed) = %+v, stream=%+v", boundary, mixed)
	}

	newTurnStreamService(a).turnStreamTracker().Streams["turn-1"] = &turnStream{TurnID: "turn-1", QuietWorking: &quietWorkingCard{}}
	executeQuietWorkingCardOp(a, context.Background(), sub, quietWorkingCardOp{
		TurnID: "turn-1",
		Body:   "Read `quiet_mode.go`",
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(ff.replyCards))
	}
	if got := newTurnStreamService(a).turnStreamTracker().Streams["turn-1"].QuietWorking; got == nil || got.MessageID == "" || got.RenderedBody != "Read `quiet_mode.go`" {
		t.Fatalf("commitQuietWorkingCardRender(reply) = %+v", got)
	}

	newTurnStreamService(a).turnStreamTracker().Streams["turn-1"].QuietWorking = &quietWorkingCard{MessageID: "reply-card-id", RenderedBody: "before"}
	executeQuietWorkingCardOp(a, context.Background(), sub, quietWorkingCardOp{
		TurnID:    "turn-1",
		MessageID: "reply-card-id",
		Body:      "Update `quiet_mode.go`",
	})
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patchedCards = %d, want 1", len(ff.patchedCards))
	}
	if got := newTurnStreamService(a).turnStreamTracker().Streams["turn-1"].QuietWorking.RenderedBody; got != "Update `quiet_mode.go`" {
		t.Fatalf("commitQuietWorkingCardRender(patch) = %q", got)
	}

	ff.patchCardErr = errors.New("boom")
	newTurnStreamService(a).turnStreamTracker().Streams["turn-1"].QuietWorking = &quietWorkingCard{MessageID: "reply-card-id", RenderedBody: "stable"}
	executeQuietWorkingCardOp(a, context.Background(), sub, quietWorkingCardOp{
		TurnID:    "turn-1",
		MessageID: "reply-card-id",
		Body:      "after error",
	})
	if got := newTurnStreamService(a).turnStreamTracker().Streams["turn-1"].QuietWorking.RenderedBody; got != "stable" {
		t.Fatalf("patch error should not commit render, got %q", got)
	}
}
