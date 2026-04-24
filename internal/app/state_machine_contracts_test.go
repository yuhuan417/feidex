package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
)

func TestInterruptLifecycleWaitsForTurnCompletedToFinalize(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{
		MessageID:     "msg-stop",
		ChatID:        "chat-1",
		ChatType:      "group",
		RootMessageID: "root-stop",
		UserID:        "user-1",
	}
	sessionKey := makeSessionKey(a, msg)
	sub := seedActiveSubmission(t, a, sessionKey, "thread-1", "turn-1")
	sess := a.store.GetSession(sessionKey)
	sess.Status = "turn_in_progress"
	if err := a.store.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	interruptCalls := 0
	fc.callHook = func(_ context.Context, method string, params any, _ any) error {
		if method != "turn/interrupt" {
			t.Fatalf("unexpected codex method: %s", method)
		}
		interruptCalls++
		got, _ := params.(map[string]any)
		if got["threadId"] != "thread-1" || got["turnId"] != "turn-1" {
			t.Fatalf("turn/interrupt params = %+v, want thread-1/turn-1", got)
		}
		return nil
	}

	if err := a.commandInterrupt(msg); err != nil {
		t.Fatalf("commandInterrupt() error = %v", err)
	}
	if interruptCalls != 1 {
		t.Fatalf("interrupt calls = %d, want 1", interruptCalls)
	}
	if len(ff.replyTexts) != 1 || !strings.Contains(ff.replyTexts[0], "已请求中断当前任务") {
		t.Fatalf("interrupt replyTexts = %+v, want interruption confirmation", ff.replyTexts)
	}

	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID != "turn-1" || sess.ActiveSubmissionID != sub.ID || sess.Status != "turn_in_progress" {
		t.Fatalf("session after turn/interrupt request = %+v, want active turn to remain until completion", sess)
	}
	updated := a.store.GetSubmission(sub.ID)
	if updated == nil || updated.Status != "running" || updated.Finalized {
		t.Fatalf("submission after turn/interrupt request = %+v, want running non-finalized", updated)
	}

	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"interrupted"}}`))

	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID != "" || sess.ActiveSubmissionID != "" || sess.Status != "idle" {
		t.Fatalf("session after interrupted completion = %+v, want idle cleared session", sess)
	}
	if got := a.store.GetSubmission(sub.ID); got != nil {
		t.Fatalf("submission after interrupted completion = %+v, want runtime submission cleanup", got)
	}
	if body := lastDeliveredCardMarkdown(t, ff); !strings.Contains(body, "任务已中断") {
		t.Fatalf("interrupted turn terminal card = %q, want interruption terminal text", body)
	}
}

func TestErrorNotificationKeepsSessionBoundUntilFailedCompletion(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	sess := a.store.GetSession("sess-1")
	sess.Status = "turn_in_progress"
	if err := a.store.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	handleNotification(a, "error", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","error":{"message":"boom"}}`))

	sess = a.store.GetSession("sess-1")
	if sess == nil || sess.ActiveTurnID != "turn-1" || sess.ActiveSubmissionID != sub.ID || sess.Status != "turn_in_progress" {
		t.Fatalf("session after error notification = %+v, want turn still active until turn/completed", sess)
	}
	updated := a.store.GetSubmission(sub.ID)
	if updated == nil || updated.Status != "failed" || updated.Finalized {
		t.Fatalf("submission after error notification = %+v, want failed but not finalized", updated)
	}

	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"failed"}}`))

	sess = a.store.GetSession("sess-1")
	if sess == nil || sess.ActiveTurnID != "" || sess.ActiveSubmissionID != "" || sess.Status != "idle" {
		t.Fatalf("session after failed completion = %+v, want idle cleared session", sess)
	}
	if got := a.store.GetSubmission(sub.ID); got != nil {
		t.Fatalf("submission after failed completion = %+v, want runtime submission cleanup", got)
	}
	if body := lastDeliveredCardMarkdown(t, ff); !strings.Contains(body, "boom") {
		t.Fatalf("failed turn terminal card = %q, want recorded error message", body)
	}
}

func TestPermissionsApprovalLifecycleResumesOnlyAfterServerRequestResolved(t *testing.T) {
	a, _, fc := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	handleServerRequest(a, codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"perm-1"`),
		Method: "item/permissions/requestApproval",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","reason":"need write","permissions":{"mode":"write","network":true}}`),
	})

	pending := a.store.PendingByID("perm-1")
	if pending == nil || pending.Kind != "permissions" || pending.Status != "pending" {
		t.Fatalf("pending after permissions request = %+v, want pending permissions request", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_approval" {
		t.Fatalf("submission after permissions request = %+v, want waiting_approval", updated)
	}

	resp, err := completeApprovalAction(a,&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "perm-1"},
	}, "approval.permissions.accept_session")
	if err != nil {
		t.Fatalf("completeApprovalAction(permissions) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("permissions approval response = %#v, want success", resp)
	}
	if len(fc.replies) != 1 {
		t.Fatalf("permissions replies = %+v, want 1 reply", fc.replies)
	}
	if pending := a.store.PendingByID("perm-1"); pending == nil || pending.Status != "replied" {
		t.Fatalf("pending after permissions reply = %+v, want replied", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_approval" {
		t.Fatalf("submission before serverRequest/resolved = %+v, want waiting_approval", updated)
	}

	handleNotification(a, "serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"perm-1"}`))

	if pending := a.store.PendingByID("perm-1"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after permissions resolve = %+v, want resolved", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "running" {
		t.Fatalf("submission after permissions resolve = %+v, want running", updated)
	}
}

func TestMcpElicitationURLLifecycleResumesOnlyAfterServerRequestResolved(t *testing.T) {
	a, _, fc := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	handleServerRequest(a, codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"elicit-1"`),
		Method: "mcpServer/elicitation/request",
		Params: json.RawMessage(`{"mode":"url","threadId":"thread-1","turnId":"turn-1","serverName":"srv","message":"open","url":"https://example.test"}`),
	})

	pending := a.store.PendingByID("elicit-1")
	if pending == nil || pending.Kind != "mcp_elicitation_url" || pending.Status != "pending" {
		t.Fatalf("pending after elicitation request = %+v, want pending url request", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_user_input" {
		t.Fatalf("submission after elicitation request = %+v, want waiting_user_input", updated)
	}

	resp, err := completeElicitationURLAction(a,&feishu.CardAction{
		UserID:      "user-1",
		ActionValue: map[string]any{"request_id": "elicit-1"},
	}, "elicitation_url.accept")
	if err != nil {
		t.Fatalf("completeElicitationURLAction() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("elicitation url response = %#v, want success", resp)
	}
	if len(fc.replies) != 1 {
		t.Fatalf("elicitation replies = %+v, want 1 reply", fc.replies)
	}
	if pending := a.store.PendingByID("elicit-1"); pending == nil || pending.Status != "replied" {
		t.Fatalf("pending after elicitation reply = %+v, want replied", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_user_input" {
		t.Fatalf("submission before serverRequest/resolved = %+v, want waiting_user_input", updated)
	}

	handleNotification(a, "serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"elicit-1"}`))

	if pending := a.store.PendingByID("elicit-1"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after elicitation resolve = %+v, want resolved", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "running" {
		t.Fatalf("submission after elicitation resolve = %+v, want running", updated)
	}
}

func TestDynamicToolCallServerRequestIsExplicitlyRejected(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	handleNotification(a, "item/started", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","item":{"id":"tool-1","type":"dynamicToolCall","tool":"dangerous"}}`))
	handleServerRequest(a, codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"tool-call-1"`),
		Method: "item/tool/call",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"tool-1","tool":"dangerous"}`),
	})

	if len(fc.replyErrors) != 1 {
		t.Fatalf("replyErrors = %+v, want one unsupported-server-request error", fc.replyErrors)
	}
	if fc.replyErrors[0].code != -32601 || fc.replyErrors[0].msg != "unsupported server request" {
		t.Fatalf("replyErrors[0] = %+v, want -32601 unsupported server request", fc.replyErrors[0])
	}
	if pending := a.store.PendingByID("tool-call-1"); pending != nil {
		t.Fatalf("pending dynamic tool request = %+v, want nil", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "running" || updated.Finalized {
		t.Fatalf("submission after unsupported dynamic tool request = %+v, want unchanged running submission", updated)
	}
	if len(ff.sendCards) != 0 || len(ff.replyCards) != 0 {
		t.Fatalf("dynamic tool rejection should not send cards, send=%d reply=%d", len(ff.sendCards), len(ff.replyCards))
	}
}

func lastDeliveredCardMarkdown(t *testing.T, ff *fakeFeishuClient) string {
	t.Helper()
	if len(ff.patchedCards) > 0 {
		return cardMarkdownContent(t, ff.patchedCards[len(ff.patchedCards)-1])
	}
	if len(ff.replyCards) > 0 {
		return cardMarkdownContent(t, ff.replyCards[len(ff.replyCards)-1])
	}
	if len(ff.sendCards) > 0 {
		return cardMarkdownContent(t, ff.sendCards[len(ff.sendCards)-1])
	}
	t.Fatal("expected at least one delivered card")
	return ""
}
