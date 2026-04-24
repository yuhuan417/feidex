package app

import (
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/state"
)

func TestItemStartedBindsPendingSubmissionBeforeTurnStarted(t *testing.T) {
	a, _, _ := newTestApp(t)
	sub := seedStartingSubmission(t, a, "sess-1", "sub-early", "thread-1", "")

	handleNotification(a, "item/started", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-early","item":{"id":"item-1","type":"commandExecution","command":"pwd"}}`))

	sess := a.store.GetSession("sess-1")
	if sess == nil || sess.ActiveSubmissionID != sub.ID || sess.ActiveTurnID != "turn-early" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after early item/started = %+v, want bound in-progress turn", sess)
	}
	updated := a.store.GetSubmission(sub.ID)
	if updated == nil || updated.ThreadID != "thread-1" || updated.TurnID != "turn-early" || updated.Status != "running" {
		t.Fatalf("submission after early item/started = %+v, want running bound submission", updated)
	}
	if _, pending := newRuntimeStateService(a).pendingSubmissionForThread("thread-1"); pending != nil {
		t.Fatalf("pending turn binding should be cleared after early bind, got %+v", pending)
	}
	if boundSessionKey, boundSub := newRuntimeStateService(a).boundSubmissionForTurn("turn-early"); boundSessionKey != "sess-1" || boundSub == nil || boundSub.ID != sub.ID {
		t.Fatalf("turn binding after early item/started = %q / %+v, want sess-1 / %s", boundSessionKey, boundSub, sub.ID)
	}
	if !a.sessionHasLiveThread("sess-1", "thread-1") {
		t.Fatal("early item/started should mark thread live for the session")
	}
	if newTurnStreamService(a).turnStreamTracker().streams["turn-early"] == nil {
		t.Fatal("early item/started should initialize turn stream state")
	}
}

func TestReviewLifecycleBindsAndDeliversWithoutTurnStarted(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedStartingSubmission(t, a, "sess-1", "sub-review", "thread-1", submissionKindReview)

	handleNotification(a, "item/started", json.RawMessage(`{"threadId":"thread-1","turnId":"review-turn-a","item":{"id":"enter-1","type":"enteredReviewMode"}}`))

	sess := a.store.GetSession("sess-1")
	if sess == nil || sess.ActiveSubmissionID != sub.ID || sess.ActiveTurnID != "review-turn-a" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after enteredReviewMode = %+v, want review turn bound before turn/started", sess)
	}

	handleNotification(a, "turn/started", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"persisted-turn-b"}}`))

	sess = a.store.GetSession("sess-1")
	if sess == nil || sess.ActiveTurnID != "review-turn-a" {
		t.Fatalf("review turn should stay on early bound turn after later turn/started, got %+v", sess)
	}

	handleNotification(a, "item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"review-turn-a","itemId":"exit-1","item":{"id":"exit-1","type":"exitedReviewMode","review":"Looks good overall."}}`))

	if body := lastDeliveredCardMarkdown(t, ff); !strings.Contains(body, "Looks good overall.") {
		t.Fatalf("review final card body = %q, want exitedReviewMode review text", body)
	}

	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"review-turn-a","status":"completed"}}`))

	sess = a.store.GetSession("sess-1")
	if sess == nil || sess.ActiveTurnID != "" || sess.ActiveSubmissionID != "" || sess.Status != "idle" {
		t.Fatalf("session after review completion = %+v, want idle cleared session", sess)
	}
	if got := a.store.GetSubmission(sub.ID); got != nil {
		t.Fatalf("submission after review completion = %+v, want runtime cleanup", got)
	}
}

func TestTurnItemStateMergesStartedContextAndClearsAfterCompletion(t *testing.T) {
	a, _, _ := newTestApp(t)

	newRuntimeStateService(a).noteTurnItemStarted("thread-1", "turn-1", map[string]any{
		"id":     "item-1",
		"type":   "fileChange",
		"status": "inProgress",
		"changes": []any{
			map[string]any{"path": "main.go", "kind": "update"},
		},
		"context": map[string]any{
			"started": "yes",
			"nested": map[string]any{
				"a": "1",
			},
		},
	})

	mergedRequest := newRuntimeStateService(a).mergeRequestPayloadWithTurnItem("thread-1", "turn-1", "item-1", map[string]any{
		"reason": "need review",
		"context": map[string]any{
			"decision": "pending",
			"nested": map[string]any{
				"b": "2",
			},
		},
	})
	if got := stringValue(mergedRequest["reason"]); got != "need review" {
		t.Fatalf("merged request reason = %q, want need review", got)
	}
	if changes, _ := mergedRequest["changes"].([]any); len(changes) != 1 {
		t.Fatalf("merged request changes = %+v, want started file changes", mergedRequest["changes"])
	}
	contextMap, _ := mergedRequest["context"].(map[string]any)
	if got := stringValue(contextMap["started"]); got != "yes" {
		t.Fatalf("merged request context.started = %q, want yes", got)
	}
	if got := stringValue(contextMap["decision"]); got != "pending" {
		t.Fatalf("merged request context.decision = %q, want pending", got)
	}
	nested, _ := contextMap["nested"].(map[string]any)
	if got := stringValue(nested["a"]); got != "1" || stringValue(nested["b"]) != "2" {
		t.Fatalf("merged request nested context = %+v, want both started and request keys", nested)
	}

	mismatchedThread := newRuntimeStateService(a).mergeRequestPayloadWithTurnItem("thread-2", "turn-1", "item-1", map[string]any{"reason": "other"})
	if _, ok := mismatchedThread["changes"]; ok {
		t.Fatalf("mismatched thread merge should not hydrate started state, got %+v", mismatchedThread)
	}

	completed := newRuntimeStateService(a).completeTurnItemState("thread-1", "turn-1", "item-1", map[string]any{
		"id":      "item-1",
		"type":    "fileChange",
		"status":  "completed",
		"summary": "done",
		"context": map[string]any{
			"completed": "yes",
			"nested": map[string]any{
				"c": "3",
			},
		},
	})
	if got := stringValue(completed["summary"]); got != "done" {
		t.Fatalf("completed summary = %q, want done", got)
	}
	if changes, _ := completed["changes"].([]any); len(changes) != 1 {
		t.Fatalf("completed changes = %+v, want started file changes preserved", completed["changes"])
	}
	completedContext, _ := completed["context"].(map[string]any)
	if got := stringValue(completedContext["started"]); got != "yes" || stringValue(completedContext["completed"]) != "yes" {
		t.Fatalf("completed context = %+v, want started+completed markers", completedContext)
	}
	completedNested, _ := completedContext["nested"].(map[string]any)
	if stringValue(completedNested["a"]) != "1" || stringValue(completedNested["c"]) != "3" {
		t.Fatalf("completed nested context = %+v, want merged nested state", completedNested)
	}
	if snapshot := newRuntimeStateService(a).turnItemSnapshot("thread-1", "turn-1", "item-1"); snapshot != nil {
		t.Fatalf("turn item snapshot after completion = %+v, want cleared state", snapshot)
	}
}

func seedStartingSubmission(t *testing.T, a *App, sessionKey, submissionID, threadID, kind string) *state.Submission {
	t.Helper()

	if err := a.store.UpsertSession(&state.Session{
		Key:                sessionKey,
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     threadID,
		ActiveSubmissionID: submissionID,
		OwnerUserID:        "user-1",
		ChatID:             "chat-1",
		ChatType:           "group",
		Status:             "turn_starting",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := a.store.CreateSubmission(&state.Submission{
		ID:               submissionID,
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         threadID,
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-1",
		InputText:        "pending input",
		Kind:             kind,
		Status:           "running",
	}); err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	newRuntimeStateService(a).notePendingTurnBinding(threadID, sessionKey, submissionID)
	return a.store.GetSubmission(submissionID)
}
