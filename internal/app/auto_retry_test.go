package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
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

func seedAutoRetrySession(t *testing.T, a *App, sessionKey, threadID string) *state.Session {
	t.Helper()
	sess := &state.Session{
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
	a.asyncRunner = func(fn func()) { fn() }

	scheduled := make([]scheduledRetry, 0, 4)
	newAutoRetryService(a).AutoRetryTracker().After = func(delay time.Duration, fn func()) delayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := newAutoRetryService(a).UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := "feishu:frontend:default:chat:chat-1"
	threadID := "thread-retry-1"
	sess := seedAutoRetrySession(t, a, sessionKey, threadID)
	markSessionThreadLive(a, sessionKey, threadID)
	sub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadID,
		ChatID:               sess.ChatID,
		TriggerMessageID:     "trigger-1",
		SourceRootMessageIDs: []string{sess.RootMessageID},
		Status:               "failed",
	}

	newAutoRetryService(a).ObserveAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "")

	if len(scheduled) != 1 {
		t.Fatalf("scheduled retries = %d, want 1", len(scheduled))
	}
	if scheduled[0].delay != time.Second {
		t.Fatalf("scheduled delay = %s, want %s", scheduled[0].delay, time.Second)
	}
	if snapshot, ok := newAutoRetryService(a).CurrentAutoRetryState(sessionKey); !ok {
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

	snapshot, ok := newAutoRetryService(a).CurrentAutoRetryState(sessionKey)
	if !ok {
		t.Fatal("currentAutoRetryState() missing state after firing timer")
	}
	if snapshot.RetryCount != 1 {
		t.Fatalf("retry count = %d, want 1 after firing timer", snapshot.RetryCount)
	}
	if cards := ff.replyCardsSnapshot(); len(cards) != 1 {
		t.Fatalf("reply cards = %d, want 1 waiting card", len(cards))
	} else if body := cardMarkdownContent(t, cards[0]); body == "" || !containsAll(body, "自动发送“继续”", "下一次自动重试") {
		t.Fatalf("waiting card body = %q", body)
	}
	if patched := ff.patchedCardsSnapshot(); len(patched) != 1 {
		t.Fatalf("patched cards = %d, want 1 running patch", len(patched))
	} else if body := cardMarkdownContent(t, patched[0]); body == "" || !containsAll(body, "已自动发送“继续”", "累计已重试: `1` 次") {
		t.Fatalf("running card body = %q", body)
	}
}

func TestAutoRetryTakesPriorityOverSameSessionQueue(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.asyncRunner = func(fn func()) { fn() }

	scheduled := make([]scheduledRetry, 0, 2)
	newAutoRetryService(a).AutoRetryTracker().After = func(delay time.Duration, fn func()) delayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := newAutoRetryService(a).UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := "feishu:frontend:default:chat:chat-1"
	threadID := "thread-retry-queue-1"
	sess := &state.Session{
		Key:                     sessionKey,
		WorkspaceID:             defaultWorkspaceID(a),
		ActiveThreadID:          threadID,
		ActiveThreadWorkspaceID: defaultWorkspaceID(a),
		OwnerUserID:             "user-1",
		ChatID:                  "chat-1",
		ChatType:                "p2p",
		Status:                  state.SessionStatusIdle.String(),
	}
	if err := a.store.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	markSessionThreadLive(a, sessionKey, threadID)
	queuedID, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:       sessionKey,
		WorkspaceID:      defaultWorkspaceID(a),
		ChatID:           "chat-1",
		TriggerMessageID: "later-1",
		InputText:        "later input",
		Status:           state.SubmissionStatusQueued.String(),
	})
	if err != nil {
		t.Fatalf("CreateSubmission(later) error = %v", err)
	}
	if err := a.State().QueueSubmission(sessionKey, queuedID); err != nil {
		t.Fatalf("QueueSubmission(later) error = %v", err)
	}
	updatedSess, err := a.State().UpdateSession(sessionKey, func(sess *state.Session) {
		sess.Status = state.SessionStatusQueued.String()
	})
	if err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}
	failedSub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadID,
		ChatID:               "chat-1",
		TriggerMessageID:     "trigger-1",
		SourceRootMessageIDs: []string{"trigger-1"},
		Status:               state.SubmissionStatusFailed.String(),
	}

	if !newAutoRetryService(a).ObserveAutoRetryTerminal(sessionKey, threadID, "failed", updatedSess, failedSub, "") {
		t.Fatal("ObserveAutoRetryTerminal() = false, want pending retry")
	}
	if len(scheduled) != 1 {
		t.Fatalf("scheduled retries = %d, want 1", len(scheduled))
	}
	if next := newSubmissionQueueServiceFromApp(a).NextQueuedSessionKey(sessionKey); next != "" {
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
	if snapshot, ok := newAutoRetryService(a).CurrentAutoRetryState(sessionKey); !ok {
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
	if _, ok := newAutoRetryService(a).CurrentAutoRetryState(sessionKey); ok {
		t.Fatal("currentAutoRetryState() still present after successful retry completion")
	}
	refreshed = a.State().Session(sessionKey)
	if refreshed == nil || len(refreshed.Queue) != 0 || refreshed.ActiveTurnID != "turn-later-1" {
		t.Fatalf("session after queued input resumes = %+v, want queue empty and turn-later-1 active", refreshed)
	}
}

func TestAutoRetryTakesPriorityOverGroupQueue(t *testing.T) {
	a, _, fc := newTestApp(t)
	a.asyncRunner = func(fn func()) { fn() }

	scheduled := make([]scheduledRetry, 0, 2)
	newAutoRetryService(a).AutoRetryTracker().After = func(delay time.Duration, fn func()) delayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := newAutoRetryService(a).UpdateAutoRetryEnabled(true); err != nil {
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

	queuedB, err := a.store.CreateSubmission(&state.Submission{
		SessionKey:       sessionKey,
		WorkspaceID:      defaultWorkspaceID(a),
		ChatID:           "chat-1",
		TriggerMessageID: "later-b",
		InputText:        "root b input",
		Status:           state.SubmissionStatusQueued.String(),
	})
	if err != nil {
		t.Fatalf("CreateSubmission(root-b) error = %v", err)
	}
	if err := a.State().QueueSubmission(sessionKey, queuedB); err != nil {
		t.Fatalf("QueueSubmission(root-b) error = %v", err)
	}
	failedSub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadA,
		ChatID:               "chat-1",
		TriggerMessageID:     "trigger-a",
		SourceRootMessageIDs: []string{"root-a"},
		Status:               state.SubmissionStatusFailed.String(),
	}
	updatedA := a.State().Session(sessionKey)

	if !newAutoRetryService(a).ObserveAutoRetryTerminal(sessionKey, threadA, "failed", updatedA, failedSub, "") {
		t.Fatal("ObserveAutoRetryTerminal() = false, want pending retry")
	}
	if len(scheduled) != 1 {
		t.Fatalf("scheduled retries = %d, want 1", len(scheduled))
	}
	if next := newSubmissionQueueServiceFromApp(a).NextQueuedSessionKey(sessionKey); next != "" {
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
	if _, ok := newAutoRetryService(a).CurrentAutoRetryState(sessionKey); ok {
		t.Fatal("currentAutoRetryState(root-a) still present after successful retry completion")
	}
	if sess := a.State().Session(sessionKey); sess == nil || len(sess.Queue) != 0 || sess.ActiveTurnID != "turn-root-b" {
		t.Fatalf("group session after retry completion = %+v, want queued turn active", sess)
	}
}

func TestCommandInterruptCancelsPendingAutoRetry(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.asyncRunner = func(fn func()) { fn() }

	scheduled := make([]scheduledRetry, 0, 2)
	newAutoRetryService(a).AutoRetryTracker().After = func(delay time.Duration, fn func()) delayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := newAutoRetryService(a).UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := "feishu:frontend:default:chat:chat-1"
	threadID := "thread-stop-1"
	sess := seedAutoRetrySession(t, a, sessionKey, threadID)
	markSessionThreadLive(a, sessionKey, threadID)
	sub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadID,
		ChatID:               sess.ChatID,
		TriggerMessageID:     "trigger-1",
		SourceRootMessageIDs: []string{sess.RootMessageID},
		Status:               "failed",
	}
	newAutoRetryService(a).ObserveAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "")

	msg := &feishu.InboundMessage{
		SessionKey: sessionKey,
		MessageID:  "cmd-1",
		ChatID:     sess.ChatID,
		ChatType:   sess.ChatType,
		UserID:     sess.OwnerUserID,
	}
	if err := commandInterrupt(a, msg); err != nil {
		t.Fatalf("commandInterrupt() error = %v", err)
	}
	if len(scheduled) != 1 || !scheduled[0].task.stopped {
		t.Fatalf("scheduled retry task stopped = %v, want true", len(scheduled) == 1 && scheduled[0].task.stopped)
	}
	if _, ok := newAutoRetryService(a).CurrentAutoRetryState(sessionKey); ok {
		t.Fatal("currentAutoRetryState() still present after /stop")
	}
	replies := ff.replyTextsSnapshot()
	if len(replies) == 0 || !containsAll(replies[len(replies)-1], "已停止当前 session 的自动重试") {
		t.Fatalf("reply texts = %#v", replies)
	}
}

func TestGroupTopLevelCommandInterruptCancelsPendingAutoRetryAcrossRoot(t *testing.T) {
	a, ff, fc := newTestApp(t)
	a.asyncRunner = func(fn func()) { fn() }

	scheduled := make([]scheduledRetry, 0, 2)
	newAutoRetryService(a).AutoRetryTracker().After = func(delay time.Duration, fn func()) delayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := newAutoRetryService(a).UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := makeSessionKey(a, &feishu.InboundMessage{MessageID: "msg-retry", ChatID: "chat-1", ChatType: "group", RootMessageID: "root-retry", UserID: "user-1"})
	threadID := "thread-stop-retry"
	sess := seedAutoRetrySession(t, a, sessionKey, threadID)
	markSessionThreadLive(a, sessionKey, threadID)
	sub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadID,
		ChatID:               sess.ChatID,
		TriggerMessageID:     "trigger-1",
		SourceRootMessageIDs: []string{sess.RootMessageID},
		Status:               "failed",
	}
	newAutoRetryService(a).ObserveAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "")
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
	if err := commandInterrupt(a, msg); err != nil {
		t.Fatalf("commandInterrupt() error = %v", err)
	}
	if !scheduled[0].task.stopped {
		t.Fatal("scheduled retry task was not stopped by cross-root /stop")
	}
	if _, ok := newAutoRetryService(a).CurrentAutoRetryState(sessionKey); ok {
		t.Fatal("currentAutoRetryState() still present after cross-root /stop")
	}
	replies := ff.replyTextsSnapshot()
	if len(replies) == 0 || !containsAll(replies[len(replies)-1], "已停止当前 session 的自动重试") {
		t.Fatalf("reply texts = %#v", replies)
	}
}

func TestClaudeAutoRetryStartFailureKeepsWaitingState(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.asyncRunner = func(fn func()) { fn() }
	a.cfg.Feishu.Backend = backendClaude
	a.claude = &fakeClaudeCore{
		ensureSessionSet: true,
		ensureSessionID:  "claude-session-1",
		startTurnErr:     errors.New("claude start failed"),
	}

	scheduled := make([]scheduledRetry, 0, 4)
	newAutoRetryService(a).AutoRetryTracker().After = func(delay time.Duration, fn func()) delayedTask {
		task := &fakeDelayedTask{fn: fn}
		scheduled = append(scheduled, scheduledRetry{delay: delay, task: task})
		return task
	}
	if err := newAutoRetryService(a).UpdateAutoRetryEnabled(true); err != nil {
		t.Fatalf("updateAutoRetryEnabled(true) error = %v", err)
	}

	sessionKey := "feishu:frontend:default:chat:chat-1"
	threadID := "claude-session-1"
	sess := seedAutoRetrySession(t, a, sessionKey, threadID)
	markSessionThreadLive(a, sessionKey, threadID)
	sub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          defaultWorkspaceID(a),
		ThreadID:             threadID,
		ChatID:               sess.ChatID,
		TriggerMessageID:     "trigger-1",
		SourceRootMessageIDs: []string{sess.RootMessageID},
		Status:               "failed",
	}

	newAutoRetryService(a).ObserveAutoRetryTerminal(sessionKey, threadID, "failed", sess, sub, "")
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
	snapshot, ok := newAutoRetryService(a).CurrentAutoRetryState(sessionKey)
	if !ok {
		t.Fatal("currentAutoRetryState() missing state after Claude start failure")
	}
	if snapshot.RetryCount != 0 {
		t.Fatalf("retry count = %d, want 0 after failed start", snapshot.RetryCount)
	}
	if !newAutoRetryService(a).HasPendingAutoRetry(sessionKey) {
		t.Fatal("hasPendingAutoRetry(sessionKey) = false, want true")
	}

	claude, ok := a.claude.(*fakeClaudeCore)
	if !ok {
		t.Fatalf("claude core type = %T", a.claude)
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
		if got := autoRetryDelayForStep(tc.step); got != tc.want {
			t.Fatalf("autoRetryDelayForStep(%d) = %s, want %s", tc.step, got, tc.want)
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
