package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	appapprovalview "feidex/internal/app/approvalview"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestCriticalPathApprovalResumeStartsQueuedFollowupAfterTurnCompletion(t *testing.T) {
	a, ff, fc := newTestApp(t)

	msg1 := &feishu.InboundMessage{
		MessageID: "msg-1",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
		Text:      "first task",
	}
	sessionKey := makeSessionKey(a, msg1)

	var mu sync.Mutex
	var methods []string
	var turnStartThreadIDs []string
	turnStartCount := 0
	secondTurnStarted := make(chan struct{}, 1)
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		mu.Lock()
		methods = append(methods, method)
		callNum := 0
		if method == "turn/start" {
			turnStartCount++
			callNum = turnStartCount
			if payload, ok := params.(map[string]any); ok {
				if threadID, _ := payload["threadId"].(string); threadID != "" {
					turnStartThreadIDs = append(turnStartThreadIDs, threadID)
				}
			}
		}
		mu.Unlock()

		switch method {
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
			result.Thread.Name = "Main Thread"
			result.Thread.Preview = "primary flow"
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

	a.HandleFeishuMessage(msg1)

	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "thread-1" || sess.ActiveTurnID != "turn-1" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after first message = %+v", sess)
	}
	firstSubID := sess.ActiveSubmissionID
	if firstSubID == "" {
		t.Fatalf("session missing active submission after first message: %+v", sess)
	}

	handleServerRequest(a, codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"cmd-1"`),
		Method: "item/commandExecution/requestApproval",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","command":"pwd"}`),
	})

	firstSub := a.store.GetSubmission(firstSubID)
	if firstSub == nil || firstSub.Status != "waiting_approval" {
		t.Fatalf("first submission after approval request = %+v, want waiting_approval", firstSub)
	}
	if pending := a.store.PendingByID("cmd-1"); pending == nil || pending.Status != "pending" {
		t.Fatalf("pending approval after request = %+v, want pending", pending)
	}
	if len(ff.sendCards) == 0 {
		t.Fatal("approval request should send a card")
	}

	msg2 := &feishu.InboundMessage{
		MessageID: "msg-2",
		ChatID:    msg1.ChatID,
		ChatType:  msg1.ChatType,
		UserID:    msg1.UserID,
		Text:      "follow-up task",
	}
	a.HandleFeishuMessage(msg2)

	sess = a.store.GetSession(sessionKey)
	if sess == nil || len(sess.Queue) != 1 || sess.ActiveTurnID != "turn-1" {
		t.Fatalf("session after queued follow-up = %+v", sess)
	}
	queuedSubID := sess.Queue[0]
	queuedSub := a.store.GetSubmission(queuedSubID)
	if queuedSub == nil || queuedSub.InputText != "follow-up task" || queuedSub.Status != "queued" {
		t.Fatalf("queued submission = %+v", queuedSub)
	}

	resp, err := a.ServerRequestService().CompleteApprovalAction( &feishu.CardAction{
		UserID:      msg1.UserID,
		ActionValue: map[string]any{"request_id": "cmd-1"},
	}, "approval.command.accept")
	if err != nil {
		t.Fatalf("completeApprovalAction() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("approval action response = %#v, want success", resp)
	}
	if len(fc.replies) != 1 {
		t.Fatalf("codex approval replies = %+v, want 1 reply", fc.replies)
	}
	if pending := a.store.PendingByID("cmd-1"); pending == nil || pending.Status != "replied" {
		t.Fatalf("pending approval after user reply = %+v, want replied", pending)
	}

	handleNotification(a, "serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"cmd-1"}`))

	firstSub = a.store.GetSubmission(firstSubID)
	if firstSub == nil || firstSub.Status != "running" {
		t.Fatalf("first submission after serverRequest/resolved = %+v, want running", firstSub)
	}
	select {
	case <-secondTurnStarted:
		t.Fatal("queued follow-up should not start before the first turn completes")
	default:
	}

	handleNotification(a, "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	select {
	case <-secondTurnStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("expected queued follow-up to start after first turn completion")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sess = a.store.GetSession(sessionKey)
		queuedSub = a.store.GetSubmission(queuedSubID)
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
		t.Fatalf("session after first turn completed = %+v", sess)
	}
	queuedSub = a.store.GetSubmission(queuedSubID)
	if queuedSub == nil || queuedSub.ThreadID != "thread-1" || queuedSub.TurnID != "turn-2" || queuedSub.Status != "running" {
		t.Fatalf("queued submission after restart = %+v", queuedSub)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 3 || methods[0] != "thread/start" || methods[1] != "turn/start" || methods[2] != "turn/start" {
		t.Fatalf("codex methods = %+v, want thread/start then two turn/start calls", methods)
	}
	if len(turnStartThreadIDs) != 2 || turnStartThreadIDs[0] != "thread-1" || turnStartThreadIDs[1] != "thread-1" {
		t.Fatalf("turn/start thread IDs = %+v, want thread-1 reused for both turns", turnStartThreadIDs)
	}
}

func TestCompleteMenuReviewReturnsReviewEntryCard(t *testing.T) {
	a, _, _ := newTestApp(t)

	resp, err := newMenuActionService(a).completeMenuReview(&feishu.CardAction{}, "sess-review")
	if err != nil {
		t.Fatalf("completeMenuReview() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "info" {
		t.Fatalf("menu review response = %#v, want info toast", resp)
	}
	if resp.Card == nil || resp.Card.Data == nil {
		t.Fatalf("menu review card = %#v, want raw card", resp)
	}

	card, ok := resp.Card.Data.(map[string]any)
	if !ok {
		t.Fatalf("menu review card data = %#v, want map", resp.Card.Data)
	}
	body := cardMarkdownContent(t, card)
	if !strings.Contains(body, "inline review") || !strings.Contains(body, "对比分支") || !strings.Contains(body, "审查 commit") {
		t.Fatalf("menu review body = %q, want review guidance", body)
	}
	buttons := cardButtonsForTest(card)
	if len(buttons) != 5 {
		t.Fatalf("menu review button count = %d, want 5", len(buttons))
	}
	wantActions := []string{
		"menu.review.uncommitted",
		"menu.review.base",
		"menu.review.commit",
		"menu.review.custom",
		"menu.tools",
	}
	for i, want := range wantActions {
		behaviors, _ := buttons[i]["behaviors"].([]map[string]any)
		if len(behaviors) != 1 {
			t.Fatalf("menu review button[%d] behaviors = %+v, want one callback", i, behaviors)
		}
		value, _ := behaviors[0]["value"].(map[string]any)
		if got, _ := value["action"].(string); got != want {
			t.Fatalf("menu review button[%d] action = %q, want %q", i, got, want)
		}
	}
}

func TestApprovalRequestPayloadPrefersNestedRequestAndFallsBackCleanly(t *testing.T) {
	nested := appapprovalview.ApprovalRequestPayload(&state.PendingRequest{
		PayloadJSON: `{"request":{"command":"pwd","cwd":"/tmp/work"},"body":"command approval"}`,
	})
	if nested["command"] != "pwd" || nested["cwd"] != "/tmp/work" {
		t.Fatalf("appapprovalview.ApprovalRequestPayload(nested) = %+v, want nested request payload", nested)
	}

	root := appapprovalview.ApprovalRequestPayload(&state.PendingRequest{
		PayloadJSON: `{"command":"pwd","cwd":"/tmp/work"}`,
	})
	if root["command"] != "pwd" || root["cwd"] != "/tmp/work" {
		t.Fatalf("appapprovalview.ApprovalRequestPayload(root) = %+v, want root payload", root)
	}

	if got := appapprovalview.ApprovalRequestPayload(&state.PendingRequest{PayloadJSON: "{"}); got != nil {
		t.Fatalf("appapprovalview.ApprovalRequestPayload(invalid) = %+v, want nil", got)
	}
	if got := appapprovalview.ApprovalRequestPayload(nil); got != nil {
		t.Fatalf("appapprovalview.ApprovalRequestPayload(nil) = %+v, want nil", got)
	}
}
