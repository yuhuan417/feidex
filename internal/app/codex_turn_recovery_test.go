package app

import (
	"context"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestEnqueueSubmissionReconcilesCompletedCodexTurnFromThreadRead(t *testing.T) {
	a, _, fc := newTestApp(t)

	msg1 := &feishu.InboundMessage{
		MessageID:     "msg-1",
		ChatID:        "chat-1",
		ChatType:      "group",
		RootMessageID: "root-1",
		UserID:        "user-1",
		Text:          "first task",
	}
	sessionKey := a.makeSessionKey(msg1)
	sub := seedActiveSubmission(t, a, sessionKey, "thread-1", "turn-1")
	if _, err := a.store.UpdateSession(sessionKey, func(sess *state.Session) {
		sess.ActiveThreadWorkspaceID = a.cfg.Workspaces[0].ID
	}); err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}
	a.markSessionThreadLive(sessionKey, "thread-1")
	newTurnStreamService(a).noteTurnStarted(sessionKey, sub)
	newTurnStreamService(a).turnStreamTracker().streams["turn-1"].SentFinal = true

	var methods []string
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		methods = append(methods, method)
		switch method {
		case "thread/read":
			result := out.(*codexrpc.ThreadReadResult)
			result.Thread.ID = "thread-1"
			result.Thread.Turns = []codexrpc.ThreadReadTurn{
				{ID: "turn-1", Status: "completed"},
			}
		case "turn/start":
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-2"
		default:
			t.Fatalf("unexpected codex method: %s", method)
		}
		return nil
	}

	msg2 := &feishu.InboundMessage{
		MessageID:     "msg-2",
		ChatID:        msg1.ChatID,
		ChatType:      msg1.ChatType,
		RootMessageID: msg1.RootMessageID,
		UserID:        msg1.UserID,
		Text:          "follow-up task",
	}
	if err := enqueueSubmission(a, msg2); err != nil {
		t.Fatalf("enqueueSubmission() error = %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "thread-1" || sess.ActiveTurnID != "turn-2" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after enqueue reconciliation = %+v", sess)
	}
	if got := a.store.GetSubmission(sub.ID); got != nil {
		t.Fatalf("stale submission after reconciliation = %+v, want cleaned", got)
	}
	activeSub := a.store.GetSubmission(sess.ActiveSubmissionID)
	if activeSub == nil || activeSub.InputText != "follow-up task" || activeSub.ThreadID != "thread-1" || activeSub.TurnID != "turn-2" || activeSub.Status != "running" {
		t.Fatalf("active submission after reconciliation = %+v", activeSub)
	}
	if len(methods) != 2 || methods[0] != "thread/read" || methods[1] != "turn/start" {
		t.Fatalf("codex methods = %+v, want thread/read then turn/start", methods)
	}
}

func TestCommandInterruptClearsQueueAfterReconcilingCompletedCodexTurn(t *testing.T) {
	a, ff, fc := newTestApp(t)

	msg := &feishu.InboundMessage{
		MessageID:     "msg-stop",
		ChatID:        "chat-1",
		ChatType:      "group",
		RootMessageID: "root-stop",
		UserID:        "user-1",
	}
	sessionKey := a.makeSessionKey(msg)
	sub := seedActiveSubmission(t, a, sessionKey, "thread-1", "turn-1")
	newTurnStreamService(a).noteTurnStarted(sessionKey, sub)
	newTurnStreamService(a).turnStreamTracker().streams["turn-1"].SentFinal = true

	queuedID, err := a.store.CreateSubmission(&state.Submission{
		ID:               "sub-queued",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		UserID:           "user-1",
		ChatID:           "chat-1",
		TriggerMessageID: "msg-queued",
		InputText:        "queued follow-up",
		Status:           "queued",
	})
	if err != nil {
		t.Fatalf("CreateSubmission(queued) error = %v", err)
	}
	if err := a.appState().queueSubmission(sessionKey, queuedID); err != nil {
		t.Fatalf("queueSubmission() error = %v", err)
	}

	var methods []string
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		methods = append(methods, method)
		switch method {
		case "thread/read":
			result := out.(*codexrpc.ThreadReadResult)
			result.Thread.ID = "thread-1"
			result.Thread.Turns = []codexrpc.ThreadReadTurn{
				{ID: "turn-1", Status: "completed"},
			}
		default:
			t.Fatalf("unexpected codex method: %s", method)
		}
		return nil
	}

	if err := a.commandInterrupt(msg); err != nil {
		t.Fatalf("commandInterrupt() error = %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.Status != "idle" || sess.ActiveTurnID != "" || sess.ActiveSubmissionID != "" || len(sess.Queue) != 0 {
		t.Fatalf("session after interrupt reconciliation = %+v", sess)
	}
	if got := a.store.GetSubmission(sub.ID); got != nil {
		t.Fatalf("stale running submission after interrupt reconciliation = %+v, want cleaned", got)
	}
	if got := a.store.GetSubmission(queuedID); got != nil {
		t.Fatalf("queued submission after interrupt reconciliation = %+v, want cleaned", got)
	}
	if len(methods) != 1 || methods[0] != "thread/read" {
		t.Fatalf("codex methods = %+v, want only thread/read", methods)
	}
	if len(ff.replyTexts) != 1 || !strings.Contains(ff.replyTexts[0], "已清空 1 条排队或暂存输入") {
		t.Fatalf("interrupt replyTexts = %+v, want queue cleared reply", ff.replyTexts)
	}
}
