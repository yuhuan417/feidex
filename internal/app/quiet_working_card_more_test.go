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
	if !card.replaceEntries(quietWorkingReasoningKey, []string{"思考中..."}) {
		t.Fatal("replaceEntries(reasoning) should report change")
	}
	if !card.isReasoningOnly() {
		t.Fatal("card should be reasoning-only")
	}
	if got := card.linesForPrefix(quietWorkingReasoningKey); len(got) != 1 || got[0] != "思考中..." {
		t.Fatalf("linesForPrefix(reasoning) = %#v", got)
	}
	if !card.replaceEntries("item:1", []string{"Read `a.go`", "Read `b.go`", "List `internal/app`"}) {
		t.Fatal("replaceEntries(item) should report change")
	}
	body := card.body()
	if !strings.Contains(body, "Read `a.go` `b.go`") || !strings.Contains(body, "List `internal/app`") {
		t.Fatalf("body() = %q", body)
	}
	if !card.removeEntries(quietWorkingReasoningKey) {
		t.Fatal("removeEntries(reasoning) should report change")
	}
	if card.isReasoningOnly() {
		t.Fatal("card should no longer be reasoning-only")
	}
	if card.removeEntries("missing") {
		t.Fatal("removeEntries(missing) should not report change")
	}
	if prefix := quietWorkingEntryPrefix(quietWorkingEntryKey("item:2", 3)); prefix != "item:2" {
		t.Fatalf("quietWorkingEntryPrefix() = %q", prefix)
	}
	if equalStringSlices([]string{"a"}, []string{"b"}) || !equalStringSlices([]string{"a"}, []string{"a"}) {
		t.Fatal("equalStringSlices() returned unexpected result")
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

	newTurnStreamService(a).turnStreamTracker().streams["turn-1"] = &turnStream{TurnID: "turn-1", QuietWorking: &quietWorkingCard{}}
	executeQuietWorkingCardOp(a,context.Background(), sub, quietWorkingCardOp{
		TurnID: "turn-1",
		Body:   "Read `quiet_mode.go`",
	})
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards = %d, want 1", len(ff.replyCards))
	}
	if got := newTurnStreamService(a).turnStreamTracker().streams["turn-1"].QuietWorking; got == nil || got.MessageID == "" || got.RenderedBody != "Read `quiet_mode.go`" {
		t.Fatalf("commitQuietWorkingCardRender(reply) = %+v", got)
	}

	newTurnStreamService(a).turnStreamTracker().streams["turn-1"].QuietWorking = &quietWorkingCard{MessageID: "reply-card-id", RenderedBody: "before"}
	executeQuietWorkingCardOp(a,context.Background(), sub, quietWorkingCardOp{
		TurnID:    "turn-1",
		MessageID: "reply-card-id",
		Body:      "Update `quiet_mode.go`",
	})
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patchedCards = %d, want 1", len(ff.patchedCards))
	}
	if got := newTurnStreamService(a).turnStreamTracker().streams["turn-1"].QuietWorking.RenderedBody; got != "Update `quiet_mode.go`" {
		t.Fatalf("commitQuietWorkingCardRender(patch) = %q", got)
	}

	ff.patchCardErr = errors.New("boom")
	newTurnStreamService(a).turnStreamTracker().streams["turn-1"].QuietWorking = &quietWorkingCard{MessageID: "reply-card-id", RenderedBody: "stable"}
	executeQuietWorkingCardOp(a,context.Background(), sub, quietWorkingCardOp{
		TurnID:    "turn-1",
		MessageID: "reply-card-id",
		Body:      "after error",
	})
	if got := newTurnStreamService(a).turnStreamTracker().streams["turn-1"].QuietWorking.RenderedBody; got != "stable" {
		t.Fatalf("patch error should not commit render, got %q", got)
	}
}
