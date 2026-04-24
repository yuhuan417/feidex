package app

import (
	"context"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/state"
)

func TestStartNextSubmissionUsesWorkspaceApprovalPolicyForTurnStart(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Workspaces[0].ApprovalPolicy = "never"
	a.cfg.Workspaces[0].SandboxMode = "read-only"

	sessionKey := "sess-workspace-policy"
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-workspace-policy",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-1",
		InputText:        "hello from workspace default",
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:         sessionKey,
		WorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:      "chat-1",
		ChatType:    "p2p",
		Queue:       []string{subID},
		Status:      "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	var threadParams map[string]any
	var turnParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "thread/start":
			threadParams, _ = params.(map[string]any)
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-workspace"
			return nil
		case "turn/start":
			turnParams, _ = params.(map[string]any)
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-workspace"
		}
		return nil
	}

	if err := startNextSubmission(a, sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}
	if threadParams == nil || turnParams == nil {
		t.Fatalf("captured params = thread:%+v turn:%+v, want both calls", threadParams, turnParams)
	}
	if got, _ := threadParams["approvalPolicy"].(string); got != "never" {
		t.Fatalf("thread/start approvalPolicy = %q, want never", got)
	}
	if got, _ := turnParams["approvalPolicy"].(string); got != "never" {
		t.Fatalf("turn/start approvalPolicy = %q, want never", got)
	}
	if got, _ := turnParams["sandboxPolicy"].(map[string]any); got["type"] != "readOnly" {
		t.Fatalf("turn/start sandboxPolicy = %+v, want readOnly", got)
	}
}

func TestStartNextSubmissionUsesThreadApprovalOverrideForTurnStart(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Workspaces[0].ApprovalPolicy = "on-request"
	a.cfg.Workspaces[0].SandboxMode = "workspace-write"

	sessionKey := "sess-thread-policy"
	subID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-thread-policy",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "trigger-1",
		InputText:        "hello from thread override",
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:                        sessionKey,
		WorkspaceID:                a.cfg.Workspaces[0].ID,
		ActiveThreadID:             "thread-existing",
		ActiveThreadWorkspaceID:    a.cfg.Workspaces[0].ID,
		ActiveThreadApprovalPolicy: "never",
		ActiveThreadSandboxMode:    "read-only",
		ChatID:                     "chat-1",
		ChatType:                   "p2p",
		Queue:                      []string{subID},
		Status:                     "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	a.markSessionThreadLive(sessionKey, "thread-existing")

	var turnParams map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "thread/start":
			t.Fatalf("unexpected thread/start for live thread reuse")
		case "turn/start":
			turnParams, _ = params.(map[string]any)
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-thread"
		}
		return nil
	}

	if err := startNextSubmission(a, sessionKey); err != nil {
		t.Fatalf("startNextSubmission() error = %v", err)
	}
	if turnParams == nil {
		t.Fatal("expected turn/start params to be captured")
	}
	if got, _ := turnParams["threadId"].(string); got != "thread-existing" {
		t.Fatalf("turn/start threadId = %q, want thread-existing", got)
	}
	if got, _ := turnParams["approvalPolicy"].(string); got != "never" {
		t.Fatalf("turn/start approvalPolicy = %q, want never from thread override", got)
	}
	if got, _ := turnParams["sandboxPolicy"].(map[string]any); got["type"] != "readOnly" {
		t.Fatalf("turn/start sandboxPolicy = %+v, want readOnly from thread override", got)
	}
}
