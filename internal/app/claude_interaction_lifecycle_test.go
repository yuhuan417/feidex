package app

import (
	"strings"
	"testing"

	appapproval "feidex/internal/app/approval"
	appclauderuntime "feidex/internal/app/clauderuntime"
	appmaintenance "feidex/internal/app/maintenance"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func newClaudeInteractionPending(t *testing.T, a *App, id, kind, status string) {
	t.Helper()
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID:          id,
		Backend:     backendClaude,
		Kind:        kind,
		SessionKey:  "feishu:chat:chat-1",
		ThreadID:    "claude-thread-1",
		TurnID:      "claude-turn-1",
		OwnerUserID: "user-1",
		FeishuMsgID: "msg-" + id,
		Status:      status,
		ExpiresAt:   1<<62 - 1,
	}); err != nil {
		t.Fatalf("UpsertPending(%s) error = %v", id, err)
	}
}

// A Claude approval that is still open when its turn is cleaned up must stay
// answerable: the CLI is still waiting for the control response.
func TestClaudeInteractionPendingSurvivesTurnCleanup(t *testing.T) {
	a, _, _ := newTestApp(t)
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID: "sub-1", SessionKey: "feishu:chat:chat-1", WorkspaceID: "default",
		ThreadID: "claude-thread-1", TurnID: "claude-turn-1",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	newClaudeInteractionPending(t, a, "claude-approval-1", "command", state.PendingRequestStatusPending.String())
	newClaudeInteractionPending(t, a, "claude-form-1", "tool_request_user_input_form", state.PendingRequestStatusPending.String())
	newClaudeInteractionPending(t, a, "claude-done-1", "command", state.PendingRequestStatusResolved.String())
	if err := a.store.UpsertPending(&state.PendingRequest{
		ID: "codex-approval-1", Backend: backendCodex, Kind: "command",
		SessionKey: "feishu:chat:chat-1", ThreadID: "thread-1", TurnID: "claude-turn-1",
		Status: state.PendingRequestStatusPending.String(), ExpiresAt: 1<<62 - 1,
	}); err != nil {
		t.Fatalf("UpsertPending(codex) error = %v", err)
	}
	if err := a.store.UpsertMessageLink(&state.MessageLink{MessageID: "msg-claude-approval-1", TurnID: "claude-turn-1"}); err != nil {
		t.Fatalf("UpsertMessageLink() error = %v", err)
	}

	appmaintenance.NewRuntimeMaintenanceService(a).CleanupSubmissionRuntimeState(&state.Submission{
		ID: subID, SessionKey: "feishu:chat:chat-1", ThreadID: "claude-thread-1", TurnID: "claude-turn-1",
	})

	for _, id := range []string{"claude-approval-1", "claude-form-1"} {
		if a.store.PendingByID(id) == nil {
			t.Fatalf("open Claude pending %q was deleted by turn cleanup", id)
		}
	}
	if a.store.PendingByID("claude-done-1") != nil {
		t.Fatal("resolved Claude pending should still be cleaned up")
	}
	if a.store.PendingByID("codex-approval-1") != nil {
		t.Fatal("Codex server-request pending should still be cleaned up")
	}
	if a.store.GetMessageLink("msg-claude-approval-1") == nil {
		t.Fatal("surviving Claude pending lost its message link")
	}
	if a.store.GetSubmission(subID) != nil {
		t.Fatal("submission should still be removed")
	}
}

// After the owning turn is gone the card must still resolve the approval.
func TestClaudeApprovalCardAnswerableAfterTurnCleanup(t *testing.T) {
	a, _, _ := newTestApp(t)
	claude := &fakeClaudeCore{}
	a.claude = claude
	newClaudeInteractionPending(t, a, "claude-approval-2", "command", state.PendingRequestStatusPending.String())

	appmaintenance.NewRuntimeMaintenanceService(a).CleanupSubmissionRuntimeState(&state.Submission{
		ID: "sub-2", SessionKey: "feishu:chat:chat-1", ThreadID: "claude-thread-1", TurnID: "claude-turn-1",
	})

	resp, err := a.ServerRequestService().CompleteApprovalAction(&feishu.CardAction{
		ActionValue: map[string]any{"request_id": "claude-approval-2"},
		UserID:      "user-1",
	}, "approval.command.accept")
	if err != nil {
		t.Fatalf("CompleteApprovalAction() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Content != "审批已提交" {
		t.Fatalf("toast = %#v, want 审批已提交", resp)
	}
	if len(claude.approvalCalls) != 1 || claude.approvalCalls[0].requestID != "claude-approval-2" {
		t.Fatalf("approval calls = %#v, want one call for the detached request", claude.approvalCalls)
	}
	if pending := a.store.PendingByID("claude-approval-2"); pending == nil || pending.Status != state.PendingRequestStatusResolved.String() {
		t.Fatalf("pending after answer = %#v, want resolved", pending)
	}
}

// When the session really ends the card is replaced instead of left clickable.
func TestExpireClaudeInteractionCardsPatchesCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	newClaudeInteractionPending(t, a, "claude-approval-3", "command", state.PendingRequestStatusPending.String())

	ExpireClaudeInteractionCards(a, "feishu:chat:chat-1", []string{"claude-approval-3"}, "session reset")

	pending := a.store.PendingByID("claude-approval-3")
	if pending == nil || pending.Status != state.PendingRequestStatusExpired.String() {
		t.Fatalf("pending = %#v, want expired", pending)
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched cards = %d, want 1", len(ff.patchedCards))
	}
	body := cardMarkdownContent(t, ff.patchedCards[0])
	if !strings.Contains(body, "无法继续处理") {
		t.Fatalf("expired card body = %q", body)
	}
}

// The detached delivery path must produce the same answerable card as the
// submission-bound path, just anchored to the conversation.
func TestSendDetachedApprovalCardDeliversAnswerableCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	claude := &fakeClaudeCore{}
	a.claude = claude

	target := appclauderuntime.InteractionTarget{
		SessionKey:       "feishu:chat:chat-1",
		WorkspaceID:      "default",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-1",
		UserID:           "user-1",
		ThreadID:         "claude-thread-1",
		TurnID:           "claude-turn-1",
	}
	err := newClaudeSupportService(a).SendDetachedApprovalCard("req-detached-1", target, appapproval.Presentation{
		Kind:     appapproval.KindCommand,
		ThreadID: "claude-thread-1",
		TurnID:   "claude-turn-1",
		Body:     "run ls",
		Payload:  appapproval.RequestPayload{Request: map[string]any{"command": "ls"}},
	})
	if err != nil {
		t.Fatalf("SendDetachedApprovalCard() error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply cards = %d, want the card anchored on the conversation", len(ff.replyCards))
	}
	pending := a.store.PendingByID("req-detached-1")
	if pending == nil {
		t.Fatal("detached card did not record a pending request")
	}
	if pending.Backend != backendClaude || pending.Kind != "command" || pending.TurnID != "claude-turn-1" || pending.FeishuMsgID == "" {
		t.Fatalf("pending = %#v, want a Claude command approval bound to the anchor", pending)
	}

	resp, err := a.ServerRequestService().CompleteApprovalAction(&feishu.CardAction{
		ActionValue: map[string]any{"request_id": "req-detached-1"},
		UserID:      "user-1",
	}, "approval.command.accept")
	if err != nil {
		t.Fatalf("CompleteApprovalAction() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Content != "审批已提交" {
		t.Fatalf("toast = %#v, want 审批已提交", resp)
	}
	if len(claude.approvalCalls) != 1 {
		t.Fatalf("approval calls = %#v, want the detached request to reach Claude", claude.approvalCalls)
	}
}
