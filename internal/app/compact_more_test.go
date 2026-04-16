package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"feidex/internal/state"
)

func TestStandaloneCompactionLifecycle(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sessionKey := "sess-compact"
	if err := a.store.UpsertSession(&state.Session{
		Key:            sessionKey,
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-1",
		ChatID:         "chat-1",
		ChatType:       "group",
		RootMessageID:  "root-1",
		Status:         "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, params any, _ any) error {
		if method != "thread/compact/start" {
			t.Fatalf("unexpected Call method %q", method)
		}
		payload, _ := params.(map[string]any)
		if got := strings.TrimSpace(stringValue(payload["threadId"])); got != "thread-1" {
			t.Fatalf("thread/compact/start threadId = %q", got)
		}
		return nil
	}

	sess, err := a.startThreadCompaction(sessionKey)
	if err != nil {
		t.Fatalf("startThreadCompaction() error = %v", err)
	}
	if sess == nil || sess.Status != sessionStatusCompacting {
		t.Fatalf("startThreadCompaction() = %+v", sess)
	}
	if !a.noteStandaloneCompactItemStarted("thread-1", "turn-1", map[string]any{
		"id":   "item-compact",
		"type": "contextCompaction",
	}) {
		t.Fatal("noteStandaloneCompactItemStarted() should succeed")
	}
	if updated := a.store.GetSession(sessionKey); updated == nil || updated.ActiveTurnID != "turn-1" || updated.Status != sessionStatusCompacting {
		t.Fatalf("session after bind = %+v", updated)
	}
	if !a.completeStandaloneCompactItem("thread-1", "turn-1", map[string]any{
		"id":     "item-compact",
		"type":   "contextCompaction",
		"status": "completed",
	}) {
		t.Fatal("completeStandaloneCompactItem() should succeed")
	}
	if updated := a.store.GetSession(sessionKey); updated == nil || updated.ActiveTurnID != "" || updated.Status != "idle" {
		t.Fatalf("session after finish = %+v", updated)
	}
	if len(ff.replyTexts) != 1 || ff.replyTexts[0] != "当前线程上下文已压缩完成。" {
		t.Fatalf("completeStandaloneCompactItem() notices = %#v", ff.replyTexts)
	}
}

func TestStandaloneCompactionFailureBranches(t *testing.T) {
	if sessionHasActiveWork(nil) {
		t.Fatal("sessionHasActiveWork(nil) should be false")
	}
	if !sessionHasActiveWork(&state.Session{Status: sessionStatusCompacting}) {
		t.Fatal("sessionHasActiveWork(compacting) should be true")
	}
	if !sessionHasActiveWork(&state.Session{ActiveSubmissionID: "sub-1"}) {
		t.Fatal("sessionHasActiveWork(active submission) should be true")
	}
	if sessionHasActiveWork(&state.Session{Status: "idle"}) {
		t.Fatal("sessionHasActiveWork(idle) should be false")
	}
	if got := standaloneCompactResultText("unknown"); got != "" {
		t.Fatalf("standaloneCompactResultText(unknown) = %q", got)
	}

	a, ff, fc := newTestApp(t)
	if _, err := a.startThreadCompaction("missing"); err == nil || !strings.Contains(err.Error(), "当前没有活动线程") {
		t.Fatalf("startThreadCompaction(missing) error = %v", err)
	}

	if err := a.store.UpsertSession(&state.Session{
		Key:                "sess-busy",
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     "thread-busy",
		ActiveSubmissionID: "sub-1",
		Status:             "idle",
	}); err != nil {
		t.Fatalf("UpsertSession(sess-busy) error = %v", err)
	}
	if _, err := a.startThreadCompaction("sess-busy"); err == nil || !strings.Contains(err.Error(), "当前任务仍在运行") {
		t.Fatalf("startThreadCompaction(busy) error = %v", err)
	}

	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-restore",
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-restore",
		ChatID:         "chat-restore",
		Status:         "waiting",
	}); err != nil {
		t.Fatalf("UpsertSession(sess-restore) error = %v", err)
	}
	fc.callErr = errors.New("compact boom")
	if _, err := a.startThreadCompaction("sess-restore"); err == nil || !strings.Contains(err.Error(), "compact boom") {
		t.Fatalf("startThreadCompaction(restore) error = %v", err)
	}
	if updated := a.store.GetSession("sess-restore"); updated == nil || updated.Status != "waiting" {
		t.Fatalf("session after restore = %+v", updated)
	}

	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-fail",
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-fail",
		ActiveTurnID:   "turn-fail",
		ChatID:         "chat-fail",
		ChatType:       "group",
		RootMessageID:  "root-fail",
		Status:         sessionStatusCompacting,
	}); err != nil {
		t.Fatalf("UpsertSession(sess-fail) error = %v", err)
	}
	if !a.failStandaloneCompactTurn("thread-fail", "", "boom") {
		t.Fatal("failStandaloneCompactTurn() should succeed")
	}
	if updated := a.store.GetSession("sess-fail"); updated == nil || updated.ActiveTurnID != "" || updated.Status != "idle" {
		t.Fatalf("session after fail = %+v", updated)
	}
	if len(ff.replyTexts) == 0 || !strings.Contains(ff.replyTexts[len(ff.replyTexts)-1], "boom") {
		t.Fatalf("failStandaloneCompactTurn() notices = %#v", ff.replyTexts)
	}

	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-complete",
		WorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadID: "thread-complete",
		ActiveTurnID:   "turn-complete",
		ChatID:         "chat-complete",
		Status:         sessionStatusCompacting,
	}); err != nil {
		t.Fatalf("UpsertSession(sess-complete) error = %v", err)
	}
	if !a.completeStandaloneCompactTurn("thread-complete", "") {
		t.Fatal("completeStandaloneCompactTurn() should succeed")
	}
	if updated := a.store.GetSession("sess-complete"); updated == nil || updated.ActiveTurnID != "" || updated.Status != "idle" {
		t.Fatalf("session after complete = %+v", updated)
	}
	if len(ff.sentTexts) == 0 || ff.sentTexts[len(ff.sentTexts)-1] != "当前线程上下文已压缩完成。" {
		t.Fatalf("completeStandaloneCompactTurn() sentTexts = %#v", ff.sentTexts)
	}

	a.sendStandaloneCompactResult(&state.Session{
		ChatID: "chat-interrupted",
	}, "interrupted")
	a.sendStandaloneCompactResult(&state.Session{
		ChatID: "chat-failed",
	}, "failed")
	if len(ff.sentTexts) < 3 || ff.sentTexts[len(ff.sentTexts)-2] != "当前线程上下文压缩已中断。" || ff.sentTexts[len(ff.sentTexts)-1] != "当前线程上下文压缩失败。" {
		t.Fatalf("sendStandaloneCompactResult() sentTexts = %#v", ff.sentTexts)
	}

	a.restoreStandaloneCompactSession("sess-complete", "thread-other", "idle")
	a.restoreStandaloneCompactSession("missing", "thread-missing", "idle")
}
