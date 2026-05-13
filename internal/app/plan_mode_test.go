package app

import (
	"context"
	"strings"
	"testing"

	appconvbackend "feidex/internal/app/convbackend"
	"feidex/internal/app/sessionctx"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCommandPlanOnSavesThreadCollaborationMode(t *testing.T) {
	a, ff, fc := newTestApp(t)
	a.cfg.Codex.Model = "gpt-5.4"

	msg := &feishu.InboundMessage{
		MessageID: "msg-1",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
	}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		Status:                  state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "collaborationMode/list":
			*out.(*codexrpc.CollaborationModeListResponse) = codexrpc.CollaborationModeListResponse{
				Data: []codexrpc.CollaborationModeMask{
					{Name: "Plan", Mode: stringPtr("plan"), ReasoningEffort: stringPtr("medium")},
				},
			}
			return nil
		default:
			t.Fatalf("unexpected codex Call method %q", method)
			return nil
		}
	}

	if err := commandPlan(a, msg, []string{"on"}); err != nil {
		t.Fatalf("commandPlan(on) error = %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadCollaborationMode == nil {
		t.Fatalf("session collaboration mode = %+v", sess)
	}
	if got := sess.ActiveThreadCollaborationMode.Mode; got != "plan" {
		t.Fatalf("mode = %q, want plan", got)
	}
	if got := sess.ActiveThreadCollaborationMode.Model; got != "gpt-5.4" {
		t.Fatalf("model = %q, want gpt-5.4", got)
	}
	if got := sess.ActiveThreadCollaborationMode.ReasoningEffort; got != "medium" {
		t.Fatalf("reasoning effort = %q, want medium", got)
	}
	if len(ff.replyTexts) != 1 || !strings.Contains(ff.replyTexts[0], "当前 thread 已开启") {
		t.Fatalf("replyTexts = %+v, want enabled status reply", ff.replyTexts)
	}
}

func TestCommandPlanOnRejectsWhenExperimentalAPIDisabled(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Codex.ExperimentalAPI = false

	msg := &feishu.InboundMessage{
		MessageID: "msg-1",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
	}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		Status:                  state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	err := commandPlan(a, msg, []string{"on"})
	if err == nil || !strings.Contains(err.Error(), "experimental API") {
		t.Fatalf("commandPlan(on) error = %v, want experimental API rejection", err)
	}
}

func TestCommandPlanWithoutArgsTogglesPlanMode(t *testing.T) {
	a, ff, fc := newTestApp(t)
	a.cfg.Codex.Model = "gpt-5.4"

	msg := &feishu.InboundMessage{
		MessageID: "msg-1",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
	}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		Status:                  state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		switch method {
		case "collaborationMode/list":
			*out.(*codexrpc.CollaborationModeListResponse) = codexrpc.CollaborationModeListResponse{
				Data: []codexrpc.CollaborationModeMask{
					{Name: "Plan", Mode: stringPtr("plan"), ReasoningEffort: stringPtr("medium")},
				},
			}
			return nil
		default:
			t.Fatalf("unexpected codex Call method %q", method)
			return nil
		}
	}

	if err := commandPlan(a, msg, nil); err != nil {
		t.Fatalf("commandPlan() enable error = %v", err)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadCollaborationMode == nil || sess.ActiveThreadCollaborationMode.Mode != "plan" {
		t.Fatalf("session collaboration mode after enable = %+v", sess)
	}
	if len(ff.replyTexts) != 1 || !strings.Contains(ff.replyTexts[0], "当前 thread 已开启") {
		t.Fatalf("replyTexts after enable = %+v", ff.replyTexts)
	}

	fc.callHook = func(_ context.Context, method string, _ any, _ any) error {
		t.Fatalf("unexpected codex call during plan disable: %s", method)
		return nil
	}

	if err := commandPlan(a, msg, nil); err != nil {
		t.Fatalf("commandPlan() disable error = %v", err)
	}
	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadCollaborationMode == nil {
		t.Fatalf("session collaboration mode after disable = %+v", sess)
	}
	if got := sess.ActiveThreadCollaborationMode.Mode; got != "default" {
		t.Fatalf("mode after disable = %q, want default", got)
	}
	if got := sess.ActiveThreadCollaborationMode.Model; got != "gpt-5.4" {
		t.Fatalf("model after disable = %q, want gpt-5.4", got)
	}
	snapshot, ok := sess.BackendThreads[backendCodex]
	if !ok || snapshot.CollaborationMode == nil || snapshot.CollaborationMode.Mode != "default" {
		t.Fatalf("backend snapshot after disable = %+v", sess.BackendThreads)
	}
	restored := *sess
	restored.ActiveThreadCollaborationMode = nil
	if !sessionctx.RestoreBackendThread(&restored, backendCodex) {
		t.Fatal("expected backend thread snapshot to restore")
	}
	if restored.ActiveThreadCollaborationMode == nil || restored.ActiveThreadCollaborationMode.Mode != "default" {
		t.Fatalf("restored collaboration mode = %+v, want default", restored.ActiveThreadCollaborationMode)
	}
	if len(ff.replyTexts) != 2 || ff.replyTexts[1] != "当前 thread 已关闭 `plan` collaboration mode。" {
		t.Fatalf("replyTexts after disable = %+v", ff.replyTexts)
	}
}

func TestCommandPlanOffStoresDefaultModeWhenActiveModeMissing(t *testing.T) {
	a, ff, fc := newTestApp(t)
	a.cfg.Codex.Model = "gpt-5.4"

	msg := &feishu.InboundMessage{
		MessageID: "msg-1",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
	}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		Status:                  state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, _ any, _ any) error {
		t.Fatalf("unexpected codex call during plan off: %s", method)
		return nil
	}

	if err := commandPlan(a, msg, []string{"off"}); err != nil {
		t.Fatalf("commandPlan(off) error = %v", err)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadCollaborationMode == nil {
		t.Fatalf("session collaboration mode after off = %+v", sess)
	}
	if got := sess.ActiveThreadCollaborationMode.Mode; got != "default" {
		t.Fatalf("mode after off = %q, want default", got)
	}
	if got := sess.ActiveThreadCollaborationMode.Model; got != "gpt-5.4" {
		t.Fatalf("model after off = %q, want gpt-5.4", got)
	}
	if len(ff.replyTexts) != 1 || ff.replyTexts[0] != "当前 thread 已关闭 `plan` collaboration mode。" {
		t.Fatalf("replyTexts after off = %+v", ff.replyTexts)
	}
}

func TestStartSubmissionTurnIncludesThreadCollaborationMode(t *testing.T) {
	a, _, fc := newTestApp(t)
	msg := &feishu.InboundMessage{ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
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
		t.Fatalf("UpsertSession() error = %v", err)
	}

	var captured map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/start" {
			t.Fatalf("unexpected method %q", method)
		}
		captured = params.(map[string]any)
		*out.(*codexrpc.TurnStartResult) = codexrpc.TurnStartResult{}
		out.(*codexrpc.TurnStartResult).Turn.ID = "turn-1"
		return nil
	}

	_, err := startSubmissionTurn(a, context.Background(), sessionKey, "thread-1", &state.Submission{
		ID:        "sub-1",
		InputText: "hello",
	}, a.cfg.Workspaces[0].Cwd, "never", "read-only", "", "", "")
	if err != nil {
		t.Fatalf("startSubmissionTurn() error = %v", err)
	}
	mode, ok := captured["collaborationMode"].(*codexrpc.CollaborationMode)
	if !ok || mode == nil {
		t.Fatalf("collaborationMode = %#v, want *codexrpc.CollaborationMode", captured["collaborationMode"])
	}
	if mode.Mode != "plan" || mode.Settings.Model != "gpt-5.4" {
		t.Fatalf("collaboration mode = %+v", mode)
	}
	if mode.Settings.ReasoningEffort == nil || *mode.Settings.ReasoningEffort != "medium" {
		t.Fatalf("reasoning effort = %+v", mode.Settings.ReasoningEffort)
	}
	if mode.Settings.DeveloperInstructions != nil {
		t.Fatalf("developer instructions = %+v, want nil", mode.Settings.DeveloperInstructions)
	}
}

func TestStartSubmissionTurnIncludesDefaultCollaborationModeAfterPlanDisabled(t *testing.T) {
	a, _, fc := newTestApp(t)
	msg := &feishu.InboundMessage{ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveThreadCollaborationMode: &state.SessionCollaborationMode{
			Mode:  "default",
			Model: "gpt-5.4",
		},
		Status: state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	var captured map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/start" {
			t.Fatalf("unexpected method %q", method)
		}
		captured = params.(map[string]any)
		*out.(*codexrpc.TurnStartResult) = codexrpc.TurnStartResult{}
		out.(*codexrpc.TurnStartResult).Turn.ID = "turn-1"
		return nil
	}

	_, err := startSubmissionTurn(a, context.Background(), sessionKey, "thread-1", &state.Submission{
		ID:        "sub-1",
		InputText: "hello",
	}, a.cfg.Workspaces[0].Cwd, "never", "read-only", "", "", "")
	if err != nil {
		t.Fatalf("startSubmissionTurn() error = %v", err)
	}
	mode, ok := captured["collaborationMode"].(*codexrpc.CollaborationMode)
	if !ok || mode == nil {
		t.Fatalf("collaborationMode = %#v, want *codexrpc.CollaborationMode", captured["collaborationMode"])
	}
	if mode.Mode != "default" || mode.Settings.Model != "gpt-5.4" {
		t.Fatalf("collaboration mode = %+v, want default gpt-5.4", mode)
	}
	if mode.Settings.ReasoningEffort != nil {
		t.Fatalf("reasoning effort = %+v, want nil", mode.Settings.ReasoningEffort)
	}
	if mode.Settings.DeveloperInstructions != nil {
		t.Fatalf("developer instructions = %+v, want nil", mode.Settings.DeveloperInstructions)
	}
}

func TestStartSubmissionTurnOmitsCollaborationModeByDefault(t *testing.T) {
	a, _, fc := newTestApp(t)
	msg := &feishu.InboundMessage{ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		Status:                  state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	var captured map[string]any
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/start" {
			t.Fatalf("unexpected method %q", method)
		}
		captured = params.(map[string]any)
		*out.(*codexrpc.TurnStartResult) = codexrpc.TurnStartResult{}
		out.(*codexrpc.TurnStartResult).Turn.ID = "turn-1"
		return nil
	}

	_, err := startSubmissionTurn(a, context.Background(), sessionKey, "thread-1", &state.Submission{
		ID:        "sub-1",
		InputText: "hello",
	}, a.cfg.Workspaces[0].Cwd, "never", "read-only", "", "", "")
	if err != nil {
		t.Fatalf("startSubmissionTurn() error = %v", err)
	}
	if _, ok := captured["collaborationMode"]; ok {
		t.Fatalf("collaborationMode = %#v, want omitted", captured["collaborationMode"])
	}
}

func TestResumeSelectedThreadClearsThreadCollaborationMode(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-old",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveThreadCollaborationMode: &state.SessionCollaborationMode{
			Mode:            "plan",
			Model:           "gpt-5.4",
			ReasoningEffort: "medium",
		},
		Status: state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method != "thread/resume" {
			t.Fatalf("unexpected method %q", method)
		}
		*out.(*codexrpc.ThreadStartResult) = codexrpc.ThreadStartResult{}
		out.(*codexrpc.ThreadStartResult).Thread.ID = "thread-new"
		return nil
	}

	sess := a.State().Session(sessionKey)
	_, err := conversationBackend(a).ResumeSelectedThread(sessionKey, sess, &a.cfg.Workspaces[0], appconvbackend.ThreadResumeSelection{
		ThreadID: "thread-new",
	})
	if err != nil {
		t.Fatalf("ResumeSelectedThread() error = %v", err)
	}
	updated := a.store.GetSession(sessionKey)
	if updated == nil {
		t.Fatal("missing updated session")
	}
	if updated.ActiveThreadID != "thread-new" {
		t.Fatalf("active thread = %q, want thread-new", updated.ActiveThreadID)
	}
	if updated.ActiveThreadCollaborationMode != nil {
		t.Fatalf("collaboration mode after resume = %+v, want nil", updated.ActiveThreadCollaborationMode)
	}
}

func TestForkActiveConversationClearsThreadCollaborationMode(t *testing.T) {
	a, _, fc := newTestApp(t)
	sessionKey := "sess-1"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-old",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ActiveThreadCollaborationMode: &state.SessionCollaborationMode{
			Mode:            "plan",
			Model:           "gpt-5.4",
			ReasoningEffort: "medium",
		},
		Status: state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method != "thread/fork" {
			t.Fatalf("unexpected method %q", method)
		}
		*out.(*codexrpc.ThreadStartResult) = codexrpc.ThreadStartResult{}
		out.(*codexrpc.ThreadStartResult).Thread.ID = "thread-fork"
		return nil
	}

	sess := a.State().Session(sessionKey)
	_, err := conversationBackend(a).ForkActiveConversation(sessionKey, sess, &a.cfg.Workspaces[0])
	if err != nil {
		t.Fatalf("ForkActiveConversation() error = %v", err)
	}
	updated := a.store.GetSession(sessionKey)
	if updated == nil {
		t.Fatal("missing updated session")
	}
	if updated.ActiveThreadID != "thread-fork" {
		t.Fatalf("active thread = %q, want thread-fork", updated.ActiveThreadID)
	}
	if updated.ActiveThreadCollaborationMode != nil {
		t.Fatalf("collaboration mode after fork = %+v, want nil", updated.ActiveThreadCollaborationMode)
	}
}

func stringPtr(value string) *string {
	return &value
}
