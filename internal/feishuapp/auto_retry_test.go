package feishuapp

import (
	domainbackend "feidex/internal/domain/backend"
	domainsubmission "feidex/internal/domain/submission"

	appautoretry "feidex/internal/application/autoretry"

	"context"
	"errors"
	appthreadmenu "feidex/internal/adapter/feishu/threadmenu"
	"feidex/internal/codexrpc"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeDelayedTask struct {
	stopped bool
	fn      func()
}

func (f *fakeDelayedTask) Stop() bool {
	wasActive := !f.stopped
	f.stopped = true
	return wasActive
}

func (f *fakeDelayedTask) fire() {
	if f == nil || f.stopped || f.fn == nil {
		return
	}
	f.fn()
}

type scheduledRetry struct {
	delay time.Duration
	task  *fakeDelayedTask
}

func seedAutoRetrySession(t *testing.T, a *App, sessionKey, threadID string) *conversation.Session {
	t.Helper()
	sess := &conversation.Session{
		Key:                     sessionKey,
		WorkspaceID:             defaultWorkspaceID(a),
		ActiveThreadID:          threadID,
		ActiveThreadWorkspaceID: defaultWorkspaceID(a),
		OwnerUserID:             "user-1",
		ChatID:                  "chat-1",
		ChatType:                "group",
		RootMessageID:           "root-1",
		Status:                  "idle",
	}
	if err := a.store.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	return a.State().Session(sessionKey)
}

func TestAutoRetrySchedulesAndStartsContinueSubmission(t *testing.T) {
	a, ff, fc := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	a.asyncRunner = func(fn func()) { fn() }

	scheduled := make([]scheduledRetry, 0, 4)
	a.bindings.AutoRetry.AutoRetryTracker().After = func(delay time.Duration, fn func()) appautoretry.DelayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := a.bindings.AutoRetry.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := "feishu:frontend:default:chat:chat-1"
	threadID := "thread-retry-1"
	sess := seedAutoRetrySession(t, a, sessionKey, threadID)
	markSessionThreadLive(a, sessionKey, threadID)
	sub := &domainsubmission.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadID,
		ChatID:               sess.ChatID,
		TriggerMessageID:     "trigger-1",
		SourceRootMessageIDs: []string{sess.RootMessageID},
		Status:               "failed",
	}

	a.bindings.AutoRetry.ObserveAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "", "HTTP 403 Forbidden")

	if len(scheduled) != 1 {
		t.Fatalf("scheduled retries = %d, want 1", len(scheduled))
	}
	if scheduled[0].delay != time.Second {
		t.Fatalf("scheduled delay = %s, want %s", scheduled[0].delay, time.Second)
	}
	if snapshot, ok := a.bindings.AutoRetry.CurrentAutoRetryState(sessionKey); !ok {
		t.Fatal("currentAutoRetryState() missing state")
	} else if snapshot.RetryCount != 0 {
		t.Fatalf("retry count = %d, want 0 before timer fires", snapshot.RetryCount)
	}

	startCalls := 0
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/start" {
			t.Fatalf("Call() method = %q, want turn/start", method)
		}
		startCalls++
		paramMap, ok := params.(map[string]any)
		if !ok {
			t.Fatalf("Call() params type = %T", params)
		}
		inputs, ok := paramMap["input"].([]map[string]any)
		if !ok || len(inputs) != 1 {
			t.Fatalf("turn/start input = %#v", paramMap["input"])
		}
		if got := inputs[0]["text"]; got != "继续" {
			t.Fatalf("turn/start input text = %#v, want 继续", got)
		}
		result, ok := out.(*codexrpc.TurnStartResult)
		if !ok {
			t.Fatalf("turn/start out type = %T", out)
		}
		result.Turn.ID = "turn-retry-1"
		return nil
	}

	scheduled[0].task.fire()
	if startCalls != 1 {
		t.Fatalf("turn/start calls = %d, want 1", startCalls)
	}

	snapshot, ok := a.bindings.AutoRetry.CurrentAutoRetryState(sessionKey)
	if !ok {
		t.Fatal("currentAutoRetryState() missing state after firing timer")
	}
	if snapshot.RetryCount != 1 {
		t.Fatalf("retry count = %d, want 1 after firing timer", snapshot.RetryCount)
	}
	if cards := ff.replyCardsSnapshot(); len(cards) != 1 {
		t.Fatalf("reply cards = %d, want 1 waiting card", len(cards))
	} else if body := cardMarkdownContent(t, cards[0]); body == "" || !containsAll(body, "自动发送“继续”", "下一次自动重试", "HTTP 403 Forbidden") {
		t.Fatalf("waiting card body = %q", body)
	}
	if patched := ff.patchedCardsSnapshot(); len(patched) != 1 {
		t.Fatalf("patched cards = %d, want 1 running patch", len(patched))
	} else if body := cardMarkdownContent(t, patched[0]); body == "" || !containsAll(body, "已自动发送“继续”", "累计已重试: `1` 次", "HTTP 403 Forbidden") {
		t.Fatalf("running card body = %q", body)
	}
}

func TestAutoRetryTakesPriorityOverSameSessionQueue(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	a.asyncRunner = func(fn func()) { fn() }

	scheduled := make([]scheduledRetry, 0, 2)
	a.bindings.AutoRetry.AutoRetryTracker().After = func(delay time.Duration, fn func()) appautoretry.DelayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := a.bindings.AutoRetry.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := "feishu:frontend:default:chat:chat-1"
	threadID := "thread-retry-queue-1"
	sess := &conversation.Session{
		Key:                     sessionKey,
		WorkspaceID:             defaultWorkspaceID(a),
		ActiveThreadID:          threadID,
		ActiveThreadWorkspaceID: defaultWorkspaceID(a),
		OwnerUserID:             "user-1",
		ChatID:                  "chat-1",
		ChatType:                "p2p",
		Status:                  conversation.SessionStatusIdle.String(),
	}
	if err := a.store.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	markSessionThreadLive(a, sessionKey, threadID)
	queuedID, err := a.store.CreateSubmission(&domainsubmission.Submission{
		SessionKey:       sessionKey,
		WorkspaceID:      defaultWorkspaceID(a),
		ChatID:           "chat-1",
		TriggerMessageID: "later-1",
		InputText:        "later input",
		Status:           domainsubmission.SubmissionStatusQueued.String(),
	})
	if err != nil {
		t.Fatalf("CreateSubmission(later) error = %v", err)
	}
	if err := a.State().QueueSubmission(sessionKey, queuedID); err != nil {
		t.Fatalf("QueueSubmission(later) error = %v", err)
	}
	updatedSess, err := a.State().UpdateSession(sessionKey, func(sess *conversation.Session) {
		sess.Status = conversation.SessionStatusQueued.String()
	})
	if err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}
	failedSub := &domainsubmission.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadID,
		ChatID:               "chat-1",
		TriggerMessageID:     "trigger-1",
		SourceRootMessageIDs: []string{"trigger-1"},
		Status:               domainsubmission.SubmissionStatusFailed.String(),
	}

	if !a.bindings.AutoRetry.ObserveAutoRetryTerminal(sessionKey, threadID, "failed", updatedSess, failedSub, "", "") {
		t.Fatal("ObserveAutoRetryTerminal() = false, want pending retry")
	}
	if len(scheduled) != 1 {
		t.Fatalf("scheduled retries = %d, want 1", len(scheduled))
	}
	if next := a.bindings.Submissions.NextQueuedSessionKey(sessionKey); next != "" {
		t.Fatalf("NextQueuedSessionKey() = %q, want blocked by auto retry", next)
	}

	startInputs := []string{}
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		if method != "turn/start" {
			t.Fatalf("Call() method = %q, want turn/start", method)
		}
		paramMap, ok := params.(map[string]any)
		if !ok {
			t.Fatalf("Call() params type = %T", params)
		}
		inputs, ok := paramMap["input"].([]map[string]any)
		if !ok || len(inputs) != 1 {
			t.Fatalf("turn/start input = %#v", paramMap["input"])
		}
		gotText, _ := inputs[0]["text"].(string)
		startInputs = append(startInputs, gotText)
		result, ok := out.(*codexrpc.TurnStartResult)
		if !ok {
			t.Fatalf("turn/start out type = %T", out)
		}
		switch len(startInputs) {
		case 1:
			result.Turn.ID = "turn-retry-queue-1"
		case 2:
			result.Turn.ID = "turn-later-1"
		default:
			t.Fatalf("unexpected turn/start #%d with input %q", len(startInputs), gotText)
		}
		return nil
	}
	scheduled[0].task.fire()
	if len(startInputs) != 1 || startInputs[0] != "继续" {
		t.Fatalf("turn/start inputs after retry fire = %#v, want [继续]", startInputs)
	}
	if snapshot, ok := a.bindings.AutoRetry.CurrentAutoRetryState(sessionKey); !ok {
		t.Fatal("currentAutoRetryState() missing after queued retry start")
	} else if snapshot.RetryCount != 1 {
		t.Fatalf("retry count after queued retry start = %d, want 1", snapshot.RetryCount)
	}

	refreshed := a.State().Session(sessionKey)
	if refreshed == nil || len(refreshed.Queue) != 1 || refreshed.Queue[0] != queuedID {
		t.Fatalf("queue after retry start = %#v, want queued later input retained", refreshed)
	}
	finishTurn(a, threadID, "turn-retry-queue-1", "completed")
	if len(startInputs) != 2 || startInputs[1] != "later input" {
		t.Fatalf("turn/start inputs after retry completion = %#v, want queued input to resume", startInputs)
	}
	if _, ok := a.bindings.AutoRetry.CurrentAutoRetryState(sessionKey); ok {
		t.Fatal("currentAutoRetryState() still present after successful retry completion")
	}
	refreshed = a.State().Session(sessionKey)
	if refreshed == nil || len(refreshed.Queue) != 0 || refreshed.ActiveTurnID != "turn-later-1" {
		t.Fatalf("session after queued input resumes = %+v, want queue empty and turn-later-1 active", refreshed)
	}
}

func TestAutoRetryTakesPriorityOverGroupQueue(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	a.asyncRunner = func(fn func()) { fn() }

	scheduled := make([]scheduledRetry, 0, 2)
	a.bindings.AutoRetry.AutoRetryTracker().After = func(delay time.Duration, fn func()) appautoretry.DelayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := a.bindings.AutoRetry.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := "feishu:frontend:default:chat:chat-1"
	threadA := "thread-root-a"
	sessA := seedAutoRetrySession(t, a, sessionKey, threadA)
	sessA.RootMessageID = "root-a"
	if err := a.store.UpsertSession(sessA); err != nil {
		t.Fatalf("UpsertSession(root-a) error = %v", err)
	}
	markSessionThreadLive(a, sessionKey, threadA)

	queuedB, err := a.store.CreateSubmission(&domainsubmission.Submission{
		SessionKey:       sessionKey,
		WorkspaceID:      defaultWorkspaceID(a),
		ChatID:           "chat-1",
		TriggerMessageID: "later-b",
		InputText:        "root b input",
		Status:           domainsubmission.SubmissionStatusQueued.String(),
	})
	if err != nil {
		t.Fatalf("CreateSubmission(root-b) error = %v", err)
	}
	if err := a.State().QueueSubmission(sessionKey, queuedB); err != nil {
		t.Fatalf("QueueSubmission(root-b) error = %v", err)
	}
	failedSub := &domainsubmission.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadA,
		ChatID:               "chat-1",
		TriggerMessageID:     "trigger-a",
		SourceRootMessageIDs: []string{"root-a"},
		Status:               domainsubmission.SubmissionStatusFailed.String(),
	}
	updatedA := a.State().Session(sessionKey)

	if !a.bindings.AutoRetry.ObserveAutoRetryTerminal(sessionKey, threadA, "failed", updatedA, failedSub, "", "") {
		t.Fatal("ObserveAutoRetryTerminal() = false, want pending retry")
	}
	if len(scheduled) != 1 {
		t.Fatalf("scheduled retries = %d, want 1", len(scheduled))
	}
	if next := a.bindings.Submissions.NextQueuedSessionKey(sessionKey); next != "" {
		t.Fatalf("NextQueuedSessionKey(group) = %q, want blocked by auto retry", next)
	}

	var threadStartCalls int
	var turnStartInputs []string
	var turnStartThreadIDs []string
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "thread/start":
			threadStartCalls++
			result := out.(*codexrpc.ThreadStartResult)
			result.Thread.ID = "thread-root-b-started"
			result.Thread.Name = "Root B"
			result.Thread.Preview = "root b"
		case "turn/start":
			paramMap, ok := params.(map[string]any)
			if !ok {
				t.Fatalf("turn/start params type = %T", params)
			}
			if threadID, _ := paramMap["threadId"].(string); threadID != "" {
				turnStartThreadIDs = append(turnStartThreadIDs, threadID)
			}
			inputs, ok := paramMap["input"].([]map[string]any)
			if !ok || len(inputs) != 1 {
				t.Fatalf("turn/start input = %#v", paramMap["input"])
			}
			gotText, _ := inputs[0]["text"].(string)
			turnStartInputs = append(turnStartInputs, gotText)
			result := out.(*codexrpc.TurnStartResult)
			switch gotText {
			case "继续":
				result.Turn.ID = "turn-root-a-retry"
			case "root b input":
				result.Turn.ID = "turn-root-b"
			default:
				t.Fatalf("unexpected turn/start input %q", gotText)
			}
		default:
			t.Fatalf("unexpected codex method %q", method)
		}
		return nil
	}

	scheduled[0].task.fire()
	if len(turnStartInputs) != 1 || turnStartInputs[0] != "继续" {
		t.Fatalf("turn/start inputs after retry fire = %#v, want [继续]", turnStartInputs)
	}
	if sess := a.State().Session(sessionKey); sess == nil || len(sess.Queue) != 1 || sess.ActiveTurnID != "turn-root-a-retry" {
		t.Fatalf("group session before retry completion = %+v, want retry active and queued follow-up", sess)
	}

	finishTurn(a, threadA, "turn-root-a-retry", "completed")
	if threadStartCalls != 0 {
		t.Fatalf("thread/start calls = %d, want 0 for same group session queued submission", threadStartCalls)
	}
	if len(turnStartInputs) != 2 || turnStartInputs[1] != "root b input" {
		t.Fatalf("turn/start inputs after retry completion = %#v, want root-b input", turnStartInputs)
	}
	if len(turnStartThreadIDs) != 2 || turnStartThreadIDs[0] != threadA || turnStartThreadIDs[1] != threadA {
		t.Fatalf("turn/start thread IDs = %#v, want retry thread reused for queued input", turnStartThreadIDs)
	}
	if _, ok := a.bindings.AutoRetry.CurrentAutoRetryState(sessionKey); ok {
		t.Fatal("currentAutoRetryState(root-a) still present after successful retry completion")
	}
	if sess := a.State().Session(sessionKey); sess == nil || len(sess.Queue) != 0 || sess.ActiveTurnID != "turn-root-b" {
		t.Fatalf("group session after retry completion = %+v, want queued turn active", sess)
	}
}

func TestCommandInterruptCancelsPendingAutoRetry(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	a.asyncRunner = func(fn func()) { fn() }

	scheduled := make([]scheduledRetry, 0, 2)
	a.bindings.AutoRetry.AutoRetryTracker().After = func(delay time.Duration, fn func()) appautoretry.DelayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := a.bindings.AutoRetry.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := "feishu:frontend:default:chat:chat-1"
	threadID := "thread-stop-1"
	sess := seedAutoRetrySession(t, a, sessionKey, threadID)
	markSessionThreadLive(a, sessionKey, threadID)
	sub := &domainsubmission.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadID,
		ChatID:               sess.ChatID,
		TriggerMessageID:     "trigger-1",
		SourceRootMessageIDs: []string{sess.RootMessageID},
		Status:               "failed",
	}
	a.bindings.AutoRetry.ObserveAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "", "")

	msg := &feishu.InboundMessage{
		SessionKey: sessionKey,
		MessageID:  "cmd-1",
		ChatID:     sess.ChatID,
		ChatType:   sess.ChatType,
		UserID:     sess.OwnerUserID,
	}
	if err := appthreadmenu.NewService(newThreadMenuDependencies(a)).CommandInterrupt(msg); err != nil {
		t.Fatalf("commandInterrupt() error = %v", err)
	}
	if len(scheduled) != 1 || !scheduled[0].task.stopped {
		t.Fatalf("scheduled retry task stopped = %v, want true", len(scheduled) == 1 && scheduled[0].task.stopped)
	}
	if _, ok := a.bindings.AutoRetry.CurrentAutoRetryState(sessionKey); ok {
		t.Fatal("currentAutoRetryState() still present after /stop")
	}
	replies := ff.replyTextsSnapshot()
	if len(replies) == 0 || !containsAll(replies[len(replies)-1], "已停止当前 session 的自动重试") {
		t.Fatalf("reply texts = %#v", replies)
	}
}

func TestGroupTopLevelCommandInterruptCancelsPendingAutoRetryAcrossRoot(t *testing.T) {
	a, ff, fc := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	a.asyncRunner = func(fn func()) { fn() }

	scheduled := make([]scheduledRetry, 0, 2)
	a.bindings.AutoRetry.AutoRetryTracker().After = func(delay time.Duration, fn func()) appautoretry.DelayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := a.bindings.AutoRetry.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := makeSessionKey(a, &feishu.InboundMessage{MessageID: "msg-retry", ChatID: "chat-1", ChatType: "group", RootMessageID: "root-retry", UserID: "user-1"})
	threadID := "thread-stop-retry"
	sess := seedAutoRetrySession(t, a, sessionKey, threadID)
	markSessionThreadLive(a, sessionKey, threadID)
	sub := &domainsubmission.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadID,
		ChatID:               sess.ChatID,
		TriggerMessageID:     "trigger-1",
		SourceRootMessageIDs: []string{sess.RootMessageID},
		Status:               "failed",
	}
	a.bindings.AutoRetry.ObserveAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "", "")
	if len(scheduled) != 1 {
		t.Fatalf("scheduled retries = %d, want 1 before /stop", len(scheduled))
	}
	fc.callHook = func(_ context.Context, method string, _ any, _ any) error {
		t.Fatalf("unexpected codex method during retry-only /stop: %s", method)
		return nil
	}

	msg := &feishu.InboundMessage{
		MessageID:     "cmd-stop-new-root",
		ChatID:        sess.ChatID,
		ChatType:      sess.ChatType,
		RootMessageID: "cmd-stop-new-root",
		UserID:        sess.OwnerUserID,
	}
	if err := appthreadmenu.NewService(newThreadMenuDependencies(a)).CommandInterrupt(msg); err != nil {
		t.Fatalf("commandInterrupt() error = %v", err)
	}
	if !scheduled[0].task.stopped {
		t.Fatal("scheduled retry task was not stopped by cross-root /stop")
	}
	if _, ok := a.bindings.AutoRetry.CurrentAutoRetryState(sessionKey); ok {
		t.Fatal("currentAutoRetryState() still present after cross-root /stop")
	}
	replies := ff.replyTextsSnapshot()
	if len(replies) == 0 || !containsAll(replies[len(replies)-1], "已停止当前 session 的自动重试") {
		t.Fatalf("reply texts = %#v", replies)
	}
}

func TestClaudeAutoRetryStartFailureKeepsWaitingState(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	a.asyncRunner = func(fn func()) { fn() }
	a.cfg.Feishu.Backend = domainbackend.BackendClaude
	a.runtimeOwner = testOwnerWithClaude(&fakeClaudeCore{
		ensureSessionSet: true,
		ensureSessionID:  "claude-session-1",
		startTurnErr:     errors.New("claude start failed"),
	})
	recomposeTestApp(a)

	scheduled := make([]scheduledRetry, 0, 4)
	a.bindings.AutoRetry.AutoRetryTracker().After = func(delay time.Duration, fn func()) appautoretry.DelayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := a.bindings.AutoRetry.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := "feishu:frontend:default:chat:chat-1"
	threadID := "claude-session-1"
	sess := seedAutoRetrySession(t, a, sessionKey, threadID)
	markSessionThreadLive(a, sessionKey, threadID)
	sub := &domainsubmission.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadID,
		ChatID:               sess.ChatID,
		TriggerMessageID:     "trigger-1",
		SourceRootMessageIDs: []string{sess.RootMessageID},
		Status:               "failed",
	}

	a.bindings.AutoRetry.ObserveAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "", "")
	if len(scheduled) != 1 {
		t.Fatalf("scheduled retries = %d, want 1 before timer fires", len(scheduled))
	}

	scheduled[0].task.fire()

	if len(scheduled) != 2 {
		t.Fatalf("scheduled retries = %d, want 2 after start failure reschedule", len(scheduled))
	}
	if scheduled[1].delay != time.Second {
		t.Fatalf("rescheduled delay = %s, want %s", scheduled[1].delay, time.Second)
	}
	snapshot, ok := a.bindings.AutoRetry.CurrentAutoRetryState(sessionKey)
	if !ok {
		t.Fatal("currentAutoRetryState() missing state after Claude start failure")
	}
	if snapshot.RetryCount != 0 {
		t.Fatalf("retry count = %d, want 0 after failed start", snapshot.RetryCount)
	}
	if !a.bindings.AutoRetry.HasPendingAutoRetry(sessionKey) {
		t.Fatal("hasPendingAutoRetry(sessionKey) = false, want true")
	}

	claude, ok := a.Claude().(*fakeClaudeCore)
	if !ok {
		t.Fatalf("claude core type = %T", a.Claude())
	}
	claude.mu.Lock()
	startTurnCalls := append([]fakeClaudeStartTurnCall(nil), claude.startTurnCalls...)
	claude.mu.Unlock()
	if len(startTurnCalls) != 2 {
		t.Fatalf("Claude StartTurn calls = %d, want 2", len(startTurnCalls))
	}
	for _, call := range startTurnCalls {
		if call.prompt != "继续" {
			t.Fatalf("Claude StartTurn prompt = %q, want 继续", call.prompt)
		}
	}

	if cards := ff.replyCardsSnapshot(); len(cards) != 1 {
		t.Fatalf("reply cards = %d, want 1 waiting card", len(cards))
	}
	patched := ff.patchedCardsSnapshot()
	if len(patched) != 1 {
		t.Fatalf("patched cards = %d, want 1 waiting-state patch", len(patched))
	}
	body := cardMarkdownContent(t, patched[0])
	if body == "" || !containsAll(body, "自动发送“继续”", "下一次自动重试") {
		t.Fatalf("waiting patch body = %q", body)
	}
	if strings.Contains(body, "已自动发送“继续”") {
		t.Fatalf("waiting patch body = %q, want no successful-send message", body)
	}
}

func TestAutoRetryDelayCapsAtFifteenSeconds(t *testing.T) {
	cases := []struct {
		step int
		want time.Duration
	}{
		{step: 0, want: 1 * time.Second},
		{step: 1, want: 2 * time.Second},
		{step: 2, want: 4 * time.Second},
		{step: 3, want: 8 * time.Second},
		{step: 4, want: 15 * time.Second},
		{step: 8, want: 15 * time.Second},
	}
	for _, tc := range cases {
		if got := appautoretry.DelayForStep(tc.step); got != tc.want {
			t.Fatalf("appautoretry.DelayForStep(%d) = %s, want %s", tc.step, got, tc.want)
		}
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func TestStopPreventsLateFailureFromRestartingRetry(t *testing.T) {
	for _, existingLoop := range []bool{false, true} {
		for _, missingCompletion := range []bool{false, true} {
			t.Run(fmt.Sprintf("loop_%t_missing_completion_%t", existingLoop, missingCompletion), func(t *testing.T) {
				a, _, fc := newTestApp(t)
				a.frontendID = "default"
				recomposeTestApp(a)
				retry := a.bindings.AutoRetry
				if err := retry.UpdateAutoRetryEnabled(true); err != nil {
					t.Fatal(err)
				}
				scheduled := 0
				retry.AutoRetryTracker().After = func(_ time.Duration, fn func()) appautoretry.DelayedTask {
					scheduled++
					return &fakeDelayedTask{fn: fn}
				}
				msg := &feishu.InboundMessage{ChatID: "chat-1", ChatType: "group", MessageID: "stop", UserID: "user-1"}
				key := makeSessionKey(a, msg)
				seedActiveSubmission(t, a, key, "thread-1", "turn-1")
				if existingLoop {
					retry.AutoRetryTracker().States[key] = &appautoretry.RetryState{SessionKey: key, ThreadID: "thread-1", RetryCount: 1, TimerSeq: 7}
				}
				fc.callHook = func(_ context.Context, method string, _ any, out any) error {
					switch method {
					case "turn/interrupt":
						if missingCompletion {
							return errors.New("no active turn to interrupt")
						}
						return nil
					case "thread/read":
						result := out.(*codexrpc.ThreadReadResult)
						result.Thread.ID = "thread-1"
						result.Thread.Turns = []codexrpc.ThreadReadTurn{{ID: "turn-1", Status: "failed"}}
						return nil
					default:
						t.Fatalf("unexpected backend call: %s", method)
						return nil
					}
				}
				if err := appthreadmenu.NewService(newThreadMenuDependencies(a)).CommandInterrupt(msg); err != nil {
					t.Fatal(err)
				}
				if !missingCompletion {
					if existingLoop {
						retry.RunAutoRetryTimer(key, 7)
					} // A late callback must preserve cancellation until terminal.
					if sess := a.State().Session(key); sess.ActiveTurnID != "turn-1" {
						t.Fatal("interrupt finalized before terminal notification")
					}
					finishTurn(a, "thread-1", "turn-1", "failed")
				}
				if scheduled != 0 {
					t.Fatalf("stop resurrected %d retries", scheduled)
				}
				if _, ok := retry.CurrentAutoRetryState(key); ok {
					t.Fatal("canceled retry not cleaned at terminal")
				}
				if sess := a.State().Session(key); sess.ActiveTurnID != "" {
					t.Fatalf("terminal not finalized: %+v", sess)
				}
			})
		}
	}
}

func TestStopInvalidatesAlreadyDispatchedRetryCallback(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	var callbacks []func()
	a.asyncRunner = func(fn func()) { callbacks = append(callbacks, fn) }
	retry := a.bindings.AutoRetry
	if err := retry.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatal(err)
	}
	var timers []*fakeDelayedTask
	retry.AutoRetryTracker().After = func(_ time.Duration, fn func()) appautoretry.DelayedTask {
		timer := &fakeDelayedTask{fn: fn}
		timers = append(timers, timer)
		return timer
	}
	msg := &feishu.InboundMessage{ChatID: "chat-1", ChatType: "group", MessageID: "stop", UserID: "user-1"}
	key := makeSessionKey(a, msg)
	sess := seedAutoRetrySession(t, a, key, "thread-1")
	markSessionThreadLive(a, key, "thread-1")
	if !retry.ObserveAutoRetryTerminal(key, "thread-1", "failed", sess, nil, "", "") {
		t.Fatal("retry not scheduled")
	}
	timers[0].fire() // Callback dispatched, but not run yet.
	if err := appthreadmenu.NewService(newThreadMenuDependencies(a)).CommandInterrupt(msg); err != nil {
		t.Fatal(err)
	}
	// A later independent task may fail and create a new loop for the same session.
	if !retry.ObserveAutoRetryTerminal(key, "thread-1", "failed", sess, nil, "", "") {
		t.Fatal("new retry not scheduled")
	}
	fc.callHook = func(_ context.Context, method string, _ any, _ any) error {
		t.Fatalf("stale timer started %s", method)
		return nil
	}
	callbacks[0]()
	if !retry.HasPendingAutoRetry(key) || timers[1].stopped {
		t.Fatal("stale callback consumed the new retry")
	}
	if err := appthreadmenu.NewService(newThreadMenuDependencies(a)).CommandInterrupt(msg); err != nil {
		t.Fatal(err)
	}
}

func TestStopWaitsForRetryStartupAndInterruptsStartedTurn(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	retry := a.bindings.AutoRetry
	if err := retry.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatal(err)
	}
	retry.AutoRetryTracker().After = func(_ time.Duration, fn func()) appautoretry.DelayedTask { return &fakeDelayedTask{fn: fn} }
	msg := &feishu.InboundMessage{ChatID: "chat-1", ChatType: "group", MessageID: "stop", UserID: "user-1"}
	key := makeSessionKey(a, msg)
	sess := seedAutoRetrySession(t, a, key, "thread-1")
	markSessionThreadLive(a, key, "thread-1")
	retry.ObserveAutoRetryTerminal(key, "thread-1", "failed", sess, nil, "", "")
	snapshot, _ := retry.CurrentAutoRetryState(key)
	starting, release, interrupted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		switch method {
		case "turn/start":
			close(starting)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			out.(*codexrpc.TurnStartResult).Turn.ID = "turn-retry"
			return nil
		case "turn/interrupt":
			if params.(map[string]any)["turnId"] != "turn-retry" {
				return errors.New("interrupted wrong turn")
			}
			close(interrupted)
			return nil
		default:
			return fmt.Errorf("unexpected method %s", method)
		}
	}
	retryDone := make(chan struct{})
	go func() { defer close(retryDone); retry.RunAutoRetryTimer(key, snapshot.TimerSeq) }()
	select {
	case <-starting:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- appthreadmenu.NewService(newThreadMenuDependencies(a)).CommandInterrupt(msg) }()
	close(release)
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-interrupted:
	default:
		t.Fatal("stop missed retry startup")
	}
	<-retryDone
	finishTurn(a, "thread-1", "turn-retry", "failed")
	if retry.HasPendingAutoRetry(key) {
		t.Fatal("stopped startup retried after failure")
	}
}

func TestStopDoesNotFinalizeUnconfirmedTurnAfterInterruptError(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	msg := &feishu.InboundMessage{ChatID: "chat-1", ChatType: "group", MessageID: "stop", UserID: "user-1"}
	key := makeSessionKey(a, msg)
	seedActiveSubmission(t, a, key, "thread-1", "turn-1")
	interruptErr := errors.New("no active turn to interrupt")
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		if method == "turn/interrupt" {
			return interruptErr
		}
		if method == "thread/read" {
			out.(*codexrpc.ThreadReadResult).Thread.Turns = []codexrpc.ThreadReadTurn{{ID: "turn-1", Status: "inProgress"}}
			return nil
		}
		t.Fatalf("unexpected call %s", method)
		return nil
	}
	if err := appthreadmenu.NewService(newThreadMenuDependencies(a)).CommandInterrupt(msg); !errors.Is(err, interruptErr) {
		t.Fatalf("error = %v", err)
	}
	if sess := a.State().Session(key); sess.ActiveTurnID != "turn-1" {
		t.Fatal("unconfirmed turn finalized")
	}
}

func TestAutoRetryCardUsesLatestFailureAndExplicitMissingDetails(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.frontendID = "default"
	recomposeTestApp(a)
	retry := a.bindings.AutoRetry
	retry.AutoRetryTracker().After = func(time.Duration, func()) appautoretry.DelayedTask { return &fakeDelayedTask{} }
	if err := retry.UpdateAutoRetryEnabled(true); err != nil {
		t.Fatal(err)
	}
	key := "feishu:frontend:default:chat:chat-1"
	sess := seedAutoRetrySession(t, a, key, "thread-1")
	for _, reason := range []string{"HTTP 403 Forbidden", "HTTP 501 Not Implemented", ""} {
		if !retry.ObserveAutoRetryTerminal(key, "thread-1", "failed", sess, nil, "", reason) {
			t.Fatal("retry not scheduled")
		}
		snapshot, ok := retry.CurrentAutoRetryState(key)
		if !ok || snapshot.LastError != reason {
			t.Fatalf("last error = %q, want %q", snapshot.LastError, reason)
		}
	}
	cards := ff.patchedCardsSnapshot()
	if len(cards) != 2 {
		t.Fatalf("patches = %d, want 2", len(cards))
	}
	if body := cardMarkdownContent(t, cards[0]); !strings.Contains(body, "HTTP 501") || strings.Contains(body, "HTTP 403") {
		t.Fatalf("stale error: %q", body)
	}
	if body := cardMarkdownContent(t, cards[1]); !strings.Contains(body, "后端未提供具体错误信息") || strings.Contains(body, "HTTP 501") {
		t.Fatalf("missing diagnostic fallback: %q", body)
	}
}
