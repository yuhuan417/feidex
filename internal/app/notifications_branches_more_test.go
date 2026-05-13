package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestHandleNotificationAdditionalBranches(t *testing.T) {
	a, ff, _ := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	handleNotification(a, "item/completed", json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","item":{"id":"agent-1","type":"agent_message","text":"done"}}`))
	handleNotification(a, "turn/started", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-2"}}`))
	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"failed"}}`))

	if len(ff.replyCards) == 0 && len(ff.replyTextWithIDs) == 0 {
		t.Fatal("expected completed/failed notifications to deliver output")
	}
}

func TestHandleServerRequestRoutesKnownMethods(t *testing.T) {
	a, ff, fc := newTestApp(t)
	seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	handleServerRequest(a, codexrpc.RequestEnvelope{ID: json.RawMessage(`"cmd-1"`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","command":"pwd"}`)})
	handleServerRequest(a, codexrpc.RequestEnvelope{ID: json.RawMessage(`"file-1"`), Method: "item/fileChange/requestApproval", Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-2","files":["main.go"]}`)})
	handleServerRequest(a, codexrpc.RequestEnvelope{ID: json.RawMessage(`"perm-1"`), Method: "item/permissions/requestApproval", Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-3","permissions":{"mode":"read"}}`)})
	handleServerRequest(a, codexrpc.RequestEnvelope{ID: json.RawMessage(`"input-1"`), Method: "item/tool/requestUserInput", Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-4","questions":[{"id":"q1","question":"Pick","options":[{"label":"A"}]}]}`)})
	handleServerRequest(a, codexrpc.RequestEnvelope{ID: json.RawMessage(`"elicit-1"`), Method: "mcpServer/elicitation/request", Params: json.RawMessage(`{"mode":"url","threadId":"thread-1","turnId":"turn-1","serverName":"srv","message":"open","url":"https://example.test"}`)})

	if len(ff.replyCards) < 5 {
		t.Fatalf("expected routed server requests to reply with cards, got %d", len(ff.replyCards))
	}
	if len(fc.replyErrors) != 0 {
		t.Fatalf("unexpected replyErrors for known server requests: %+v", fc.replyErrors)
	}
}

func TestFinishTurnStatuses(t *testing.T) {
	a, _, _ := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")
	sub.Status = "running"
	if err := a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
		s.Status = "running"
	}); err != nil {
		t.Fatalf("UpdateSubmission() error = %v", err)
	}

	finishTurn(a, "thread-1", "turn-1", "interrupted")
	if sess := a.store.GetSession("sess-1"); sess == nil || sess.Status != "idle" {
		t.Fatalf("session after interrupted finish = %+v", sess)
	}

	finishTurn(a, "missing", "missing", "failed")
}

func TestStandaloneCompactItemLifecycleTracksSessionState(t *testing.T) {
	a, ff, _ := newTestApp(t)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-compact",
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-compact",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:                  "chat-compact",
		ChatType:                "p2p",
		Status:                  sessionStatusCompacting,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	handleNotification(a, "item/started", json.RawMessage(`{"threadId":"thread-compact","turnId":"turn-compact","item":{"id":"item-compact","type":"contextCompaction"}}`))
	if sess := a.store.GetSession("sess-compact"); sess == nil || sess.ActiveTurnID != "turn-compact" || sess.Status != sessionStatusCompacting {
		t.Fatalf("session after item/started = %+v", sess)
	}
	handleNotification(a, "item/completed", json.RawMessage(`{"threadId":"thread-compact","turnId":"turn-compact","itemId":"item-compact","item":{"id":"item-compact","type":"contextCompaction","status":"completed"}}`))
	sess := a.store.GetSession("sess-compact")
	if sess == nil || sess.ActiveTurnID != "" || sess.Status != "idle" {
		t.Fatalf("session after item/completed = %+v", sess)
	}
	if len(ff.sentTexts) == 0 || !strings.Contains(ff.sentTexts[0], "压缩完成") {
		t.Fatalf("compact completion notice = %#v, want visible completion text", ff.sentTexts)
	}
}

func TestStandaloneCompactNotificationsCanArriveBeforeRPCReturns(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-compact", ChatID: "chat-compact", ChatType: "p2p", UserID: "user-1"}
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-compact",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:                  msg.ChatID,
		ChatType:                msg.ChatType,
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	fc.callHook = func(_ context.Context, method string, _ any, _ any) error {
		if method != "thread/compact/start" {
			return nil
		}
		handleNotification(a, "item/started", json.RawMessage(`{"threadId":"thread-compact","turnId":"turn-compact","item":{"id":"item-compact","type":"contextCompaction"}}`))
		handleNotification(a, "item/completed", json.RawMessage(`{"threadId":"thread-compact","turnId":"turn-compact","itemId":"item-compact","item":{"id":"item-compact","type":"contextCompaction","status":"completed"}}`))
		handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-compact","turn":{"id":"turn-compact","status":"completed"}}`))
		return nil
	}

	if err := commandCompact(a, msg, nil); err != nil {
		t.Fatalf("commandCompact() error = %v", err)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.Status != "idle" || sess.ActiveTurnID != "" {
		t.Fatalf("session after raced compact notifications = %+v", sess)
	}
	if len(ff.sentTexts) == 0 || !strings.Contains(ff.sentTexts[0], "压缩完成") {
		t.Fatalf("raced compact completion notice = %#v, want visible completion text", ff.sentTexts)
	}
}

func TestStandaloneCompactSuccessIgnoresLaterFailedCompletion(t *testing.T) {
	a, ff, _ := newTestApp(t)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-compact",
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-compact",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:                  "chat-compact",
		ChatType:                "p2p",
		Status:                  sessionStatusCompacting,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	handleNotification(a, "item/started", json.RawMessage(`{"threadId":"thread-compact","turnId":"turn-compact","item":{"id":"item-compact","type":"contextCompaction"}}`))
	handleNotification(a, "item/completed", json.RawMessage(`{"threadId":"thread-compact","turnId":"turn-compact","itemId":"item-compact","item":{"id":"item-compact","type":"contextCompaction","status":"completed"}}`))
	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-compact","turn":{"id":"turn-compact","status":"failed"}}`))

	if len(ff.sentTexts) != 1 || !strings.Contains(ff.sentTexts[0], "压缩完成") {
		t.Fatalf("compact notifications should only report success once, got %#v", ff.sentTexts)
	}
}

func TestStandaloneCompactErrorReportsRealMessage(t *testing.T) {
	a, ff, _ := newTestApp(t)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-compact",
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "thread-compact",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		ChatID:                  "chat-compact",
		ChatType:                "p2p",
		Status:                  sessionStatusCompacting,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	handleNotification(a, "error", json.RawMessage(`{"threadId":"thread-compact","turnId":"turn-compact","error":{"message":"context window not eligible"}}`))

	sess := a.store.GetSession("sess-compact")
	if sess == nil || sess.Status != "idle" || sess.ActiveTurnID != "" {
		t.Fatalf("session after compact error = %+v", sess)
	}
	if len(ff.sentTexts) != 1 || !strings.Contains(ff.sentTexts[0], "context window not eligible") {
		t.Fatalf("compact error notice = %#v, want real error message", ff.sentTexts)
	}
}

func TestNotificationHelperWrappers(t *testing.T) {
	a, ff, fc := newTestApp(t)
	sub := seedActiveSubmission(t, a, "sess-1", "thread-1", "turn-1")

	a.ServerRequestService().SendPermissionsCard(json.RawMessage(`"perm-wrap"`), "thread-1", "turn-1", "item-1", "body", map[string]any{"mode": "read"})
	if pending := a.store.PendingByID("perm-wrap"); pending == nil || pending.Kind != "permissions" {
		t.Fatalf("sendPermissionsCard() pending = %+v", pending)
	}
	if len(ff.replyCards) == 0 {
		t.Fatal("sendPermissionsCard() should reply with a card")
	}

	fc.replyErrors = nil
	onFileApproval(a, codexrpc.RequestEnvelope{ID: json.RawMessage(`"bad-file"`), Params: json.RawMessage(`{`)})
	onPermissionsApproval(a, codexrpc.RequestEnvelope{ID: json.RawMessage(`"bad-perm"`), Params: json.RawMessage(`{`)})
	onToolUserInput(a, codexrpc.RequestEnvelope{ID: json.RawMessage(`"bad-input"`), Params: json.RawMessage(`{`)})
	onMcpElicitationRequest(a, codexrpc.RequestEnvelope{ID: json.RawMessage(`"bad-json"`), Params: json.RawMessage(`{`)})
	if len(fc.replyErrors) < 4 {
		t.Fatalf("expected invalid request params to reply with errors, got %+v", fc.replyErrors)
	}

	_ = sub
}
