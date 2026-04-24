package app

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
)

func TestConfiguredSessionInflightModeCodexWSRemainsSingle(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Codex.Transport = "ws"

	if got := a.configuredSessionInflightMode(); got != sessionInflightSingle {
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
	sessionKey := a.makeSessionKey(msg1)

	var mu sync.Mutex
	var methods []string
	turnStartCount := 0
	secondTurnStarted := make(chan struct{}, 1)
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
				select {
				case secondTurnStarted <- struct{}{}:
				default:
				}
			default:
				t.Fatalf("unexpected turn/start call #%d", callNum)
			}
		default:
			t.Fatalf("unexpected codex method: %s", method)
		}
		return nil
	}

	a.handleFeishuMessage(msg1)

	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID != "turn-1" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after first message = %+v", sess)
	}
	firstSubID := sess.ActiveSubmissionID
	if firstSubID == "" {
		t.Fatalf("session missing active submission after first message: %+v", sess)
	}

	a.handleFeishuMessage(&feishu.InboundMessage{
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

	select {
	case <-secondTurnStarted:
		t.Fatal("follow-up should stay queued until the first turn completes")
	default:
	}

	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	select {
	case <-secondTurnStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected queued follow-up to start after first turn completion")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sess = a.store.GetSession(sessionKey)
		queuedSub := a.store.GetSubmission(queuedSubID)
		if sess != nil &&
			queuedSub != nil &&
			sess.ActiveSubmissionID == queuedSubID &&
			sess.ActiveTurnID == "turn-2" &&
			sess.Status == "turn_in_progress" &&
			queuedSub.ThreadID == "thread-1" &&
			queuedSub.TurnID == "turn-2" &&
			queuedSub.Status == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

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
