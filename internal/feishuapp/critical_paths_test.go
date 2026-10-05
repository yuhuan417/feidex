package feishuapp

import (
	"context"
	"encoding/json"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"strings"
	"sync"
	"testing"
	"time"

	appapprovalview "feidex/internal/adapter/feishu/approvalview"
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
	sessionKey := a.configView().makeSessionKey(msg1)

	var mu sync.Mutex
	var methods []string
	var turnStartThreadIDs []string
	turnStartCount := 0
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

	handleServerRequest(a.BackendRuntimeDeps(), codexrpc.RequestEnvelope{
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
	if len(ff.replyCards) == 0 {
		t.Fatal("approval request should reply with a card")
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

	resp, err := a.bindings.ServerRequests.CompleteApprovalAction(&feishu.CardAction{
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

	handleNotification(a.BackendRuntimeDeps(), "serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"cmd-1"}`))

	firstSub = a.store.GetSubmission(firstSubID)
	if firstSub == nil || firstSub.Status != "running" {
		t.Fatalf("first submission after serverRequest/resolved = %+v, want running", firstSub)
	}
	handleNotification(a.BackendRuntimeDeps(), "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	a.waitAsync()

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

// --- merged from critical_paths_more_test.go ---

func seedActiveSubmissionForInboundMessage(t *testing.T, a *Frontend, msg *feishu.InboundMessage, threadID, turnID string) (string, *domainsubmission.Submission) {
	t.Helper()

	sessionKey := a.configView().makeSessionKey(msg)
	if err := a.store.UpsertSession(&conversation.Session{
		Key:                sessionKey,
		WorkspaceID:        a.cfg.Workspaces[0].ID,
		ActiveThreadID:     threadID,
		ActiveTurnID:       turnID,
		ActiveSubmissionID: "sub-1",
		OwnerUserID:        msg.UserID,
		ChatID:             msg.ChatID,
		ChatType:           msg.ChatType,
		Status:             "running",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	subID, err := a.store.CreateSubmission(&domainsubmission.Submission{
		ID:               "sub-1",
		SessionKey:       sessionKey,
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         threadID,
		TurnID:           turnID,
		UserID:           msg.UserID,
		ChatID:           msg.ChatID,
		TriggerMessageID: "trigger-1",
		Status:           "running",
	})
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	markSessionThreadLive(a, sessionKey, threadID)
	return sessionKey, a.store.GetSubmission(subID)
}

func TestDelayedTurnStartedNotificationBindsPendingSubmissionAndStartsQueuedFollowup(t *testing.T) {
	a, _, fc := newTestApp(t)

	msg1 := &feishu.InboundMessage{
		MessageID: "msg-timeout-1",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
		Text:      "first task",
	}
	sessionKey := a.configView().makeSessionKey(msg1)

	var mu sync.Mutex
	var methods []string
	turnStartCalls := 0
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		mu.Lock()
		methods = append(methods, method)
		callNum := 0
		if method == "turn/start" {
			turnStartCalls++
			callNum = turnStartCalls
		}
		mu.Unlock()

		switch method {
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
		case "turn/start":
			if callNum == 1 {
				return context.DeadlineExceeded
			}
			if callNum != 2 {
				t.Fatalf("unexpected turn/start call #%d", callNum)
			}
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-2"
		default:
			t.Fatalf("unexpected method: %s", method)
		}
		return nil
	}

	a.HandleFeishuMessage(msg1)

	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "thread-1" || sess.ActiveTurnID != "" || sess.ActiveSubmissionID == "" || sess.Status != "turn_starting" {
		t.Fatalf("session after timed-out turn/start = %+v", sess)
	}
	firstSubID := sess.ActiveSubmissionID
	firstSub := a.store.GetSubmission(firstSubID)
	if firstSub == nil || firstSub.ThreadID != "thread-1" || firstSub.TurnID != "" || firstSub.Status != "running" {
		t.Fatalf("submission after timed-out turn/start = %+v", firstSub)
	}

	msg2 := &feishu.InboundMessage{
		MessageID: "msg-timeout-2",
		ChatID:    msg1.ChatID,
		ChatType:  msg1.ChatType,
		UserID:    msg1.UserID,
		Text:      "queued follow-up",
	}
	a.HandleFeishuMessage(msg2)

	sess = a.store.GetSession(sessionKey)
	if sess == nil || len(sess.Queue) != 1 || sess.ActiveSubmissionID != firstSubID {
		t.Fatalf("session after queued follow-up = %+v", sess)
	}
	queuedSubID := sess.Queue[0]

	handleNotification(a.BackendRuntimeDeps(), "turn/started", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1"}}`))

	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID != "turn-1" || sess.ActiveSubmissionID != firstSubID || sess.Status != "turn_in_progress" {
		t.Fatalf("session after delayed turn/started = %+v", sess)
	}
	firstSub = a.store.GetSubmission(firstSubID)
	if firstSub == nil || firstSub.TurnID != "turn-1" || firstSub.Status != "running" {
		t.Fatalf("submission after delayed turn/started = %+v", firstSub)
	}

	handleNotification(a.BackendRuntimeDeps(), "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	time.Sleep(50 * time.Millisecond)

	deadline := time.Now().Add(5 * time.Second)
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
		t.Fatalf("queued submission after delayed turn completed = %+v", queuedSub)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 3 || methods[0] != "thread/start" || methods[1] != "turn/start" || methods[2] != "turn/start" {
		t.Fatalf("methods = %+v, want thread/start then two turn/start calls", methods)
	}
}

func TestTurnCompletedWithoutStartedNotificationFinishesPendingSubmissionAndStartsQueuedFollowup(t *testing.T) {
	a, _, fc := newTestApp(t)

	msg1 := &feishu.InboundMessage{
		MessageID: "msg-complete-1",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
		Text:      "first task",
	}
	sessionKey := a.configView().makeSessionKey(msg1)

	var mu sync.Mutex
	var methods []string
	turnStartCalls := 0
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		mu.Lock()
		methods = append(methods, method)
		callNum := 0
		if method == "turn/start" {
			turnStartCalls++
			callNum = turnStartCalls
		}
		mu.Unlock()

		switch method {
		case "thread/start":
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-1"
		case "turn/start":
			if callNum == 1 {
				return context.DeadlineExceeded
			}
			if callNum != 2 {
				t.Fatalf("unexpected turn/start call #%d", callNum)
			}
			result := out.(*codexrpc.TurnStartResult)
			result.Turn.ID = "turn-2"
		default:
			t.Fatalf("unexpected method: %s", method)
		}
		return nil
	}

	a.HandleFeishuMessage(msg1)

	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveThreadID != "thread-1" || sess.ActiveTurnID != "" || sess.ActiveSubmissionID == "" || sess.Status != "turn_starting" {
		t.Fatalf("session after timed-out turn/start = %+v", sess)
	}
	firstSubID := sess.ActiveSubmissionID

	msg2 := &feishu.InboundMessage{
		MessageID: "msg-complete-2",
		ChatID:    msg1.ChatID,
		ChatType:  msg1.ChatType,
		UserID:    msg1.UserID,
		Text:      "queued follow-up",
	}
	a.HandleFeishuMessage(msg2)

	sess = a.store.GetSession(sessionKey)
	if sess == nil || len(sess.Queue) != 1 || sess.ActiveSubmissionID != firstSubID {
		t.Fatalf("session after queued follow-up = %+v", sess)
	}
	queuedSubID := sess.Queue[0]

	handleNotification(a.BackendRuntimeDeps(), "turn/completed", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}`))

	time.Sleep(50 * time.Millisecond)

	deadline := time.Now().Add(5 * time.Second)
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

	if firstSub := a.store.GetSubmission(firstSubID); firstSub != nil {
		t.Fatalf("first submission after completion = %+v, want runtime cleanup", firstSub)
	}
	sess = a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveSubmissionID != queuedSubID || sess.ActiveTurnID != "turn-2" || sess.Status != "turn_in_progress" {
		t.Fatalf("session after queued follow-up started = %+v", sess)
	}
	queuedSub := a.store.GetSubmission(queuedSubID)
	if queuedSub == nil || queuedSub.ThreadID != "thread-1" || queuedSub.TurnID != "turn-2" || queuedSub.Status != "running" {
		t.Fatalf("queued submission after completion fallback = %+v", queuedSub)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 3 || methods[0] != "thread/start" || methods[1] != "turn/start" || methods[2] != "turn/start" {
		t.Fatalf("methods = %+v, want thread/start then two turn/start calls", methods)
	}
}

func TestToolUserInputFormFlowRepliesByTextAndResumesAfterServerResolution(t *testing.T) {
	a, ff, fc := newTestApp(t)

	msg := &feishu.InboundMessage{
		MessageID: "msg-user-input-context",
		ChatID:    "chat-input",
		ChatType:  "p2p",
		UserID:    "user-1",
	}
	sessionKey, sub := seedActiveSubmissionForInboundMessage(t, a, msg, "thread-1", "turn-1")

	handleServerRequest(a.BackendRuntimeDeps(), codexrpc.RequestEnvelope{
		ID:     json.RawMessage(`"input-form-1"`),
		Method: "item/tool/requestUserInput",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","questions":[{"id":"mode","question":"Pick mode","options":[{"label":"Fast"},{"label":"Safe"},{"label":"Balanced"},{"label":"Thorough"}]}]}`),
	})

	pending := a.store.PendingByID("input-form-1")
	if pending == nil || pending.Kind != "tool_request_user_input_form" || pending.Status != "pending" {
		t.Fatalf("pending after requestUserInput = %+v, want pending form", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_user_input" {
		t.Fatalf("submission after requestUserInput = %+v, want waiting_user_input", updated)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("user input form cards = %d, want 1", len(ff.replyCards))
	}

	a.HandleFeishuMessage(&feishu.InboundMessage{
		MessageID: "msg-user-input-answer",
		ChatID:    msg.ChatID,
		ChatType:  msg.ChatType,
		UserID:    msg.UserID,
		Text:      "Fast",
	})

	if len(fc.replies) != 1 {
		t.Fatalf("codex replies after text answer = %+v, want 1 reply", fc.replies)
	}
	replyPayload, _ := fc.replies[0].result.(map[string]any)
	answers, _ := replyPayload["answers"].(map[string]any)
	mode, _ := answers["mode"].(map[string]any)
	modeAnswers, _ := mode["answers"].([]string)
	if len(modeAnswers) != 1 || modeAnswers[0] != "Fast" {
		t.Fatalf("tool user input answers = %+v, want Fast", replyPayload)
	}
	if pending := a.store.PendingByID("input-form-1"); pending == nil || pending.Status != "replied" {
		t.Fatalf("pending after text answer = %+v, want replied", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "waiting_user_input" {
		t.Fatalf("submission before serverRequest/resolved = %+v, want waiting_user_input", updated)
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched cards after text answer = %d, want 1", len(ff.patchedCards))
	}
	if sess := a.store.GetSession(sessionKey); sess == nil || len(sess.Queue) != 0 || sess.ActiveSubmissionID != sub.ID {
		t.Fatalf("session after text answer = %+v, want same in-flight submission", sess)
	}

	handleNotification(a.BackendRuntimeDeps(), "serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":"input-form-1"}`))

	if pending := a.store.PendingByID("input-form-1"); pending == nil || pending.Status != "resolved" {
		t.Fatalf("pending after serverRequest/resolved = %+v, want resolved", pending)
	}
	if updated := a.store.GetSubmission(sub.ID); updated == nil || updated.Status != "running" {
		t.Fatalf("submission after serverRequest/resolved = %+v, want running", updated)
	}
}
