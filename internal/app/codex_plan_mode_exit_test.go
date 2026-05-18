package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/app/sessionctx"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
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
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1 final plan prompt only", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count = %d, want 0 without prior live plan card", len(ff.patchedCards))
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
	for _, card := range ff.replyCards {
		if strings.Contains(cardHeaderTitle(t, card), "最终答复") {
			t.Fatalf("unexpected empty final fallback card: %#v", card)
		}
	}
}

func TestCodexPlanModeExitPromptReusesLivePlanCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	seedPlanExitActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	handleNotification(a, "turn/plan/updated", json.RawMessage(`{"turnId":"turn-1","plan":[{"step":"draft checklist","status":"completed"}]}`))
	handleNotification(a, "item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"plan-1","item":{"id":"plan-1","type":"plan","text":"1. edit files\n2. run tests"}}`))
	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1 live plan card", len(ff.replyCards))
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched card count = %d, want 1 final prompt patch", len(ff.patchedCards))
	}
	initial := ff.replyCards[0]
	if got := cardHeaderTitle(t, initial); got != "["+a.cfg.Workspaces[0].ID+"] [plan] 计划更新" {
		t.Fatalf("initial live plan title = %q", got)
	}
	if body := cardMarkdownContent(t, initial); !strings.Contains(body, "draft checklist") {
		t.Fatalf("initial live plan body = %q", body)
	}
	finalCard := ff.patchedCards[0]
	if got := cardHeaderTitle(t, finalCard); got != "["+a.cfg.Workspaces[0].ID+"] [plan] Implement this plan?" {
		t.Fatalf("patched final prompt title = %q", got)
	}
	body := cardMarkdownContent(t, finalCard)
	if !strings.Contains(body, "1. edit files") || !strings.Contains(body, "2. run tests") {
		t.Fatalf("patched final prompt body = %q", body)
	}
	if strings.Contains(body, "draft checklist") {
		t.Fatalf("patched final prompt should use plan item text, got %q", body)
	}
	for _, label := range []string{"Yes, implement this plan", "Yes, clear context and implement", "No, stay in Plan mode"} {
		if !cardHasButtonText(finalCard, label) {
			t.Fatalf("patched final prompt missing button %q: %#v", label, cardButtonsForTest(finalCard))
		}
	}
	if pending := codexPlanModeExitPendingRequest(a, "sess-1"); pending == nil {
		t.Fatal("expected codex plan-mode exit pending request")
	} else if pending.FeishuMsgID != "reply-card-id" {
		t.Fatalf("pending.FeishuMsgID = %q, want reply-card-id reused from live plan card", pending.FeishuMsgID)
	}
	for _, card := range append([]map[string]any(nil), ff.replyCards...) {
		if strings.Contains(cardHeaderTitle(t, card), "最终答复") {
			t.Fatalf("unexpected empty final fallback card: %#v", card)
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
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1 final plan content fallback", len(ff.replyCards))
	}
	card := ff.replyCards[0]
	if got := cardHeaderTitle(t, card); got != "["+a.cfg.Workspaces[0].ID+"] [plan] 计划更新" {
		t.Fatalf("final fallback plan title = %q", got)
	}
	if body := cardMarkdownContent(t, card); !strings.Contains(body, "implement x") {
		t.Fatalf("final fallback plan body = %q", body)
	}
	if cardHasButtonText(card, "Yes, implement this plan") {
		t.Fatalf("final fallback plan card should not include prompt buttons: %#v", cardButtonsForTest(card))
	}
	for _, card := range ff.replyCards {
		if cardHasButtonText(card, "Yes, implement this plan") {
			t.Fatalf("plan exit prompt should not be sent when another pending existed at completion: %#v", card)
		}
	}
}

func TestCodexPlanModeExitStayKeepsOriginalPromptCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	seedPlanExitActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	handleNotification(a, "item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"plan-1","item":{"id":"plan-1","type":"plan","text":"1. edit files\n2. run tests"}}`))
	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	pending := codexPlanModeExitPendingRequest(a, "sess-1")
	if pending == nil {
		t.Fatal("expected codex plan-mode exit pending request")
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("initial reply card count = %d, want 1 prompt card", len(ff.replyCards))
	}
	promptCard := ff.replyCards[0]
	if got := cardHeaderTitle(t, promptCard); got != "["+a.cfg.Workspaces[0].ID+"] [plan] Implement this plan?" {
		t.Fatalf("initial prompt title = %q", got)
	}

	resp, err := completeCodexPlanModeExit(a, &feishu.CardAction{
		UserID:      "user-1",
		MessageID:   pending.FeishuMsgID,
		ActionValue: map[string]any{"request_id": pending.ID},
	}, codexPlanModeExitStayAction)
	if err != nil {
		t.Fatalf("completeCodexPlanModeExit(stay) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "info" {
		t.Fatalf("completeCodexPlanModeExit(stay) response = %#v, want info toast only", resp)
	}
	if resp.Card != nil {
		t.Fatalf("completeCodexPlanModeExit(stay) should not patch original card, got card %#v", resp.Card)
	}

	a.waitAsync()

	if len(ff.patchedCards) != 0 {
		t.Fatalf("patched card count = %d, want 0", len(ff.patchedCards))
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after stay = %d, want prompt + follow-up", len(ff.replyCards))
	}
	stillPrompt := ff.replyCards[0]
	if got := cardHeaderTitle(t, stillPrompt); got != "["+a.cfg.Workspaces[0].ID+"] [plan] Implement this plan?" {
		t.Fatalf("preserved prompt title = %q", got)
	}
	if !cardHasButtonText(stillPrompt, "No, stay in Plan mode") {
		t.Fatalf("preserved prompt buttons = %#v", cardButtonsForTest(stillPrompt))
	}
	followup := ff.replyCards[1]
	if got := cardHeaderTitle(t, followup); got != "["+a.cfg.Workspaces[0].ID+"] [plan] Plan mode kept" {
		t.Fatalf("follow-up title = %q", got)
	}
	if body := cardMarkdownContent(t, followup); !strings.Contains(body, "Stayed in Plan mode.") {
		t.Fatalf("follow-up body = %q", body)
	}
	refreshed := a.State().Pending(pending.ID)
	if refreshed == nil || refreshed.Status != state.PendingRequestStatusResolved.String() {
		t.Fatalf("pending after stay = %+v, want resolved", refreshed)
	}
}

func TestCodexPlanModeExitImplementCurrentFollowupReplySteersActiveTurn(t *testing.T) {
	a, ff, fc := newTestApp(t)
	ff.replyCardIDs = []string{"plan-prompt", "plan-followup"}
	seedPlanExitActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	markSessionThreadLive(a, "sess-1", "thread-1")

	handleNotification(a, "item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"plan-1","item":{"id":"plan-1","type":"plan","text":"1. edit files\n2. run tests"}}`))
	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	pending := codexPlanModeExitPendingRequest(a, "sess-1")
	if pending == nil {
		t.Fatal("expected codex plan-mode exit pending request")
	}
	if pending.FeishuMsgID != "plan-prompt" {
		t.Fatalf("pending.FeishuMsgID = %q, want plan-prompt", pending.FeishuMsgID)
	}

	steerCalls := 0
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "turn/start":
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-2"
			return nil
		case "turn/steer":
			steerCalls++
			got, _ := params.(map[string]any)
			if got["threadId"] != "thread-1" || got["expectedTurnId"] != "turn-2" {
				t.Fatalf("turn/steer params = %+v", got)
			}
			return nil
		default:
			return nil
		}
	}

	resp, err := completeCodexPlanModeExit(a, &feishu.CardAction{
		UserID:      "user-1",
		MessageID:   pending.FeishuMsgID,
		ActionValue: map[string]any{"request_id": pending.ID},
	}, codexPlanModeExitImplementCurrentAction)
	if err != nil {
		t.Fatalf("completeCodexPlanModeExit(implement current) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "info" {
		t.Fatalf("completeCodexPlanModeExit(implement current) response = %#v, want info toast only", resp)
	}
	if resp.Card != nil {
		t.Fatalf("completeCodexPlanModeExit(implement current) should not patch original card, got card %#v", resp.Card)
	}

	a.waitAsync()

	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after implement current = %d, want prompt + follow-up", len(ff.replyCards))
	}
	followup := ff.replyCards[1]
	if got := cardHeaderTitle(t, followup); got != "["+a.cfg.Workspaces[0].ID+"] Plan implementation started" {
		t.Fatalf("follow-up title = %q", got)
	}
	link := a.State().MessageLink("plan-followup")
	if link == nil {
		t.Fatal("expected follow-up message link")
	}
	if link.SessionKey != "sess-1" || link.ThreadID != "thread-1" || link.TurnID != "turn-2" {
		t.Fatalf("follow-up message link = %+v, want sess-1/thread-1/turn-2", link)
	}

	a.HandleFeishuMessage(&feishu.InboundMessage{
		MessageID:       "reply-1",
		ChatID:          "chat-1",
		ChatType:        "group",
		UserID:          "user-1",
		Text:            "follow up on implementation",
		RootMessageID:   "plan-followup",
		ParentMessageID: "plan-followup",
	})

	if steerCalls != 1 {
		t.Fatalf("steer calls = %d, want 1", steerCalls)
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

func TestClearCodexPlanModeForSessionRestoresConfiguredDefaultEffort(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Codex.Model = "gpt-5.4"
	a.cfg.Codex.ReasoningEffort = "xhigh"
	sessionKey := "sess-1"
	if err := a.State().SaveSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveThreadCollaborationMode: &state.SessionCollaborationMode{
			Mode:            "plan",
			Model:           "gpt-5.5",
			ReasoningEffort: "xhigh",
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
	mode := codexCollaborationModeForTurnStart(a, sessionKey, "thread-1")
	if mode == nil || mode.Mode != "default" || mode.Settings.Model != "gpt-5.4" {
		t.Fatalf("turn/start collaboration mode after clear = %+v, want default gpt-5.4", mode)
	}
	if mode.Settings.ReasoningEffort == nil || *mode.Settings.ReasoningEffort != "xhigh" {
		t.Fatalf("reasoning effort after clear = %+v, want xhigh", mode.Settings.ReasoningEffort)
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
