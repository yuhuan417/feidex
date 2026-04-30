package app

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
)

func TestConfiguredSessionInflightModeCodexWSRemainsSingle(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Codex.Transport = "ws"

	if got := configuredSessionInflightMode(a); got != sessionInflightSingle {
		t.Fatalf("configuredSessionInflightMode() = %q, want %q", got, sessionInflightSingle)
	}
}

func TestHandleFeishuMessageCodexWSQueuesFollowupUntilTurnCompletion(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.cfg.Codex.Transport = "ws"

	msg1 := &feishu.InboundMessage{
		MessageID: "msg-ws-1",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
		Text:      "first task",
	}
	sessionKey := makeSessionKey(a, msg1)

	var mu sync.Mutex
	var methods []string
	turnStartCount := 0
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		mu.Lock()
		methods = append(methods, method)
		callNum := 0
		if method == "turn/start" {
			turnStartCount++
			callNum = turnStartCount
		}
		mu.Unlock()

		switch method {
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
		case "turn/start":
			result := out.(*codexrpc.TurnStartResult)
			switch callNum {
			case 1:
				result.Turn.ID = "turn-1"
			case 2:
				result.Turn.ID = "turn-2"
			default:
				t.Fatalf("unexpected turn/start call #%d", callNum)
			}
		default:
			t.Fatalf("unexpected codex method: %s", method)
		}
		return nil
	}

	a.HandleFeishuMessage(msg1)

	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID != "turn-1" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after first message = %+v", sess)
	}
	firstSubID := sess.ActiveSubmissionID
	if firstSubID == "" {
		t.Fatalf("session missing active submission after first message: %+v", sess)
	}

	a.HandleFeishuMessage(&feishu.InboundMessage{
		MessageID: "msg-ws-2",
		ChatID:    msg1.ChatID,
		ChatType:  msg1.ChatType,
		UserID:    msg1.UserID,
		Text:      "follow-up task",
	})

	sess = a.store.GetSession(sessionKey)
	if sess == nil || len(sess.Queue) != 1 || sess.ActiveSubmissionID != firstSubID || sess.ActiveTurnID != "turn-1" || sess.Status != "queued" {
		t.Fatalf("session after queued follow-up = %+v", sess)
	}
	queuedSubID := sess.Queue[0]

	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	a.waitAsync()

	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveSubmissionID != queuedSubID || sess.ActiveTurnID != "turn-2" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after queued follow-up started = %+v", sess)
	}
	queuedSub := a.store.GetSubmission(queuedSubID)
	if queuedSub == nil || queuedSub.ThreadID != "thread-1" || queuedSub.TurnID != "turn-2" || queuedSub.Status != "running" {
		t.Fatalf("queued submission after first turn completion = %+v", queuedSub)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 3 || methods[0] != "thread/start" || methods[1] != "turn/start" || methods[2] != "turn/start" {
		t.Fatalf("codex methods = %+v, want thread/start then two turn/start calls", methods)
	}
}
