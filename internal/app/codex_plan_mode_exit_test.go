package app

import (
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/app/sessionctx"
	"feidex/internal/state"
)

func TestCodexPlanModeExitPromptAfterPlanItemCompletion(t *testing.T) {
	a, ff, _ := newTestApp(t)
	seedPlanExitActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	handleNotification(a, "item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"plan-1","item":{"id":"plan-1","type":"plan","text":"1. edit files\n2. run tests"}}`))
	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	pending := codexPlanModeExitPendingRequest(a, "sess-1")
	if pending == nil {
		titles := make([]string, 0, len(ff.replyCards))
		for _, card := range ff.replyCards {
			titles = append(titles, cardHeaderTitle(t, card)+": "+cardMarkdownContent(t, card))
		}
		t.Fatalf("expected codex plan-mode exit pending request; backend=%q sess=%+v pending=%+v cards=%d titles=%q", configuredBackend(a), a.State().Session("sess-1"), a.State().PendingRequests(), len(ff.replyCards), titles)
	}
	if got := cardHeaderTitle(t, ff.replyCards[len(ff.replyCards)-1]); got != "["+a.cfg.Workspaces[0].ID+"] [plan] Implement this plan?" {
		t.Fatalf("plan exit prompt title = %q", got)
	}
	if body := cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1]); !strings.Contains(body, "1. edit files") || !strings.Contains(body, "2. run tests") {
		t.Fatalf("plan exit prompt body = %q", body)
	}
	card := ff.replyCards[len(ff.replyCards)-1]
	for _, label := range []string{"Yes, implement this plan", "Yes, clear context and implement", "No, stay in Plan mode"} {
		if !cardHasButtonText(card, label) {
			t.Fatalf("plan exit prompt missing button %q: %#v", label, cardButtonsForTest(card))
		}
	}
}

func TestCodexPlanModeExitIgnoresChecklistOnlyPlanUpdates(t *testing.T) {
	a, ff, _ := newTestApp(t)
	seedPlanExitActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	handleNotification(a, "turn/plan/updated", json.RawMessage(`{"turnId":"turn-1","plan":[{"step":"draft","status":"completed"}]}`))
	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	if pending := codexPlanModeExitPendingRequest(a, "sess-1"); pending != nil {
		t.Fatalf("checklist-only turn created plan exit pending = %+v", pending)
	}
	for _, card := range ff.replyCards {
		if cardHasButtonText(card, "Yes, implement this plan") {
			t.Fatalf("checklist-only turn should not render plan exit prompt: %#v", card)
		}
	}
}

func TestCodexPlanModeExitDoesNotDeferWhenOtherPendingExists(t *testing.T) {
	a, ff, _ := newTestApp(t)
	seedPlanExitActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	if err := a.State().SavePending(&state.PendingRequest{
		ID:         "cmd-1",
		Kind:       "command",
		SessionKey: "sess-1",
		ThreadID:   "thread-1",
		TurnID:     "turn-1",
		Status:     state.PendingRequestStatusPending.String(),
	}); err != nil {
		t.Fatalf("SavePending() error = %v", err)
	}

	handleNotification(a, "item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"plan-1","item":{"id":"plan-1","type":"plan","text":"implement x"}}`))
	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))
	handleNotification(a, "serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"cmd-1"}`))

	if pending := codexPlanModeExitPendingRequest(a, "sess-1"); pending != nil {
		t.Fatalf("plan exit should not be deferred after other pending resolves: %+v", pending)
	}
	for _, card := range ff.replyCards {
		if cardHasButtonText(card, "Yes, implement this plan") {
			t.Fatalf("plan exit prompt should not be sent when another pending existed at completion: %#v", card)
		}
	}
}

func TestClearCodexPlanModeForSessionStoresDefaultCollaborationMode(t *testing.T) {
	a, _, _ := newTestApp(t)
	sessionKey := "sess-1"
	if err := a.State().SaveSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveThreadCollaborationMode: &state.SessionCollaborationMode{
			Mode:            "plan",
			Model:           "gpt-5.4",
			ReasoningEffort: "medium",
		},
		Status: state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	cleared, err := clearCodexPlanModeForSession(a, sessionKey)
	if err != nil {
		t.Fatalf("clearCodexPlanModeForSession() error = %v", err)
	}
	if !cleared {
		t.Fatal("clearCodexPlanModeForSession() cleared = false, want true")
	}
	sess := a.State().Session(sessionKey)
	if sess == nil || sess.ActiveThreadCollaborationMode == nil {
		t.Fatalf("session collaboration mode after clear = %+v", sess)
	}
	if got := sess.ActiveThreadCollaborationMode.Mode; got != "default" {
		t.Fatalf("mode after clear = %q, want default", got)
	}
	if got := sess.ActiveThreadCollaborationMode.Model; got != "gpt-5.4" {
		t.Fatalf("model after clear = %q, want gpt-5.4", got)
	}
	snapshot, ok := sess.BackendThreads[backendCodex]
	if !ok || snapshot.CollaborationMode == nil || snapshot.CollaborationMode.Mode != "default" {
		t.Fatalf("backend snapshot after clear = %+v", sess.BackendThreads)
	}
	mode := codexCollaborationModeForTurnStart(a, sessionKey, "thread-1")
	if mode == nil || mode.Mode != "default" || mode.Settings.Model != "gpt-5.4" {
		t.Fatalf("turn/start collaboration mode after clear = %+v, want default", mode)
	}
	if mode.Settings.ReasoningEffort != nil {
		t.Fatalf("reasoning effort after clear = %+v, want nil", mode.Settings.ReasoningEffort)
	}
}

func seedPlanExitActiveSubmission(t *testing.T, a *App, sessionKey, threadID, turnID string) *state.Submission {
	t.Helper()
	sub := seedActiveSubmission(t, a, sessionKey, threadID, turnID)
	if _, err := a.State().UpdateSession(sessionKey, func(sess *state.Session) {
		if sess == nil {
			return
		}
		sess.ActiveThreadWorkspaceID = a.cfg.Workspaces[0].ID
		sess.ActiveThreadCollaborationMode = &state.SessionCollaborationMode{
			Mode:  "plan",
			Model: "gpt-5.4",
		}
		sess.ActiveOperations = []state.SessionActiveOperation{{
			Kind:         sessionctx.OpKindSubmission,
			SubmissionID: sub.ID,
			ThreadID:     threadID,
			TurnID:       turnID,
		}}
	}); err != nil {
		t.Fatalf("UpdateSession(active ops) error = %v", err)
	}
	return sub
}
