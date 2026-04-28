package app

import (
	"strings"
	"testing"

	"feidex/internal/state"
)

func TestStartNextClaudeSubmissionFailsGracefullyWhenRuntimeUnavailable(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Feishu.Backend = backendClaude

	sessionKey := "feishu:frontend:default:p2p:chat-1:user-1"
	sess := &state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID: "user-1",
		ChatID:      "chat-1",
		ChatType:    "p2p",
		Status:      "idle",
	}
	if err := a.store.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-claude-1",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "msg-1",
		SourceMessageIDs: []string{"msg-1"},
		InputText:        "hello",
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	sub := a.store.GetSubmission(subID)
	if sub == nil {
		t.Fatal("expected queued submission")
	}

	err = newSubmissionQueueServiceFromApp(a).StartNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, &a.cfg.Workspaces[0], false)
	if err == nil || !strings.Contains(err.Error(), "claude backend not initialized") {
		t.Fatalf("startNextClaudeSubmissionWithFailureNotice() error = %v, want claude backend not initialized", err)
	}

	if got := a.store.GetSubmission(subID); got != nil {
		t.Fatalf("submission should be cleaned up after graceful failure, got %+v", got)
	}
	updatedSess := a.store.GetSession(sessionKey)
	if updatedSess == nil {
		t.Fatal("expected session to remain after graceful failure")
	}
	if updatedSess.Status != "idle" {
		t.Fatalf("session status = %q, want idle", updatedSess.Status)
	}
	if updatedSess.ActiveThreadID != "" || updatedSess.ActiveTurnID != "" || updatedSess.ActiveSubmissionID != "" {
		t.Fatalf("session active state should be cleared, got %+v", updatedSess)
	}
}
