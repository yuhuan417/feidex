package feishuapp

import (
	"errors"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"strings"
	"testing"
)

func TestSubmissionBackendRuntimeAdapterTracksSelectedBackend(t *testing.T) {
	a, _, _ := newTestApp(t)
	selectBackendForTest(a, domainbackend.BackendCodex)
	adapter := sqBackendRuntimeAdapter{deps: a.BackendRuntimeDeps(), backendOwner: a.runtimeOwner}
	startFailure := errors.New("codex client not initialized")

	if !adapter.DropThreadLineageAfterStartFailure(startFailure) {
		t.Fatal("Codex adapter should drop thread lineage after a missing client")
	}

	selectBackendForTest(a, domainbackend.BackendClaude)
	if adapter.DropThreadLineageAfterStartFailure(startFailure) {
		t.Fatal("adapter should use the currently selected Claude backend")
	}
}

func TestStartNextClaudeSubmissionFailsGracefullyWhenRuntimeUnavailable(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	a.cfg.Feishu.Backend = domainbackend.BackendClaude

	sessionKey := "feishu:frontend:default:chat:chat-1"
	sess := &conversation.Session{
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
	subID, err := a.store.CreateSubmission(&domainsubmission.Submission{
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

	err = a.bindings.Submissions.StartNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, &a.cfg.Workspaces[0], false)
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
