package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"feidex/internal/app/sessionctx"
	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func seedGoalTestSession(t *testing.T, a *App, msg *feishu.InboundMessage, threadID string) string {
	t.Helper()
	sessionKey := makeSessionKey(a, msg)
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          threadID,
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID:             msg.UserID,
		ChatID:                  msg.ChatID,
		ChatType:                msg.ChatType,
		RootMessageID:           msg.MessageID,
		Status:                  state.SessionStatusIdle.String(),
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	return sessionKey
}

func testThreadGoal(threadID, objective string, status codexrpc.ThreadGoalStatus) codexrpc.ThreadGoal {
	return codexrpc.ThreadGoal{
		ThreadID:        threadID,
		Objective:       objective,
		Status:          status,
		TokensUsed:      1200,
		TimeUsedSeconds: 90,
		CreatedAt:       1,
		UpdatedAt:       2,
	}
}

func goalFormForTest(t *testing.T, card map[string]any) map[string]any {
	t.Helper()
	for _, elem := range cardElementsForTest(card) {
		if tag, _ := elem["tag"].(string); tag == "form" {
			if name, _ := elem["name"].(string); name == "goal_edit_form" {
				return elem
			}
		}
	}
	t.Fatalf("goal form missing from card: %#v", card)
	return nil
}

func goalFormInputsForTest(t *testing.T, form map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	elements, _ := form["elements"].([]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "input" {
			continue
		}
		name, _ := elem["name"].(string)
		if name != "" {
			out[name] = elem
		}
	}
	return out
}

func TestCommandGoalStatusAndSetUseCodexGoalRPC(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-goal", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	seedGoalTestSession(t, a, msg, "thread-1")

	budget := int64(200000)
	current := testThreadGoal("thread-1", "keep latency below 120ms", codexrpc.ThreadGoalStatusPaused)
	current.TokenBudget = &budget
	currentGet := &current
	var calls []string
	var setParams []codexrpc.ThreadGoalSetParams
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		calls = append(calls, method)
		switch method {
		case "thread/goal/get":
			raw, _ := params.(map[string]any)
			if raw["threadId"] != "thread-1" {
				t.Fatalf("thread/goal/get params = %#v", params)
			}
			out.(*codexrpc.ThreadGoalGetResponse).Goal = currentGet
		case "thread/goal/set":
			p, ok := params.(codexrpc.ThreadGoalSetParams)
			if !ok {
				t.Fatalf("thread/goal/set params type = %T", params)
			}
			setParams = append(setParams, p)
			status := codexrpc.ThreadGoalStatusActive
			if p.Status != nil {
				status = *p.Status
			}
			objective := current.Objective
			if p.Objective != nil {
				objective = *p.Objective
			}
			out.(*codexrpc.ThreadGoalSetResponse).Goal = testThreadGoal(p.ThreadID, objective, status)
		default:
			t.Fatalf("unexpected Codex method: %s", method)
		}
		return nil
	}

	if err := handleCommand(a, msg, "/goal"); err != nil {
		t.Fatalf("handleCommand(/goal) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("replyCards after /goal = %d, want 1", len(ff.replyCards))
	}
	if body := cardMarkdownContent(t, ff.replyCards[0]); !strings.Contains(body, "keep latency below 120ms") || !strings.Contains(body, "token budget: `200.0K`") {
		t.Fatalf("/goal card body = %q", body)
	}

	calls = nil
	setParams = nil
	currentGet = nil
	objective := "--tokens 98.5K improve benchmark coverage"
	if err := handleCommand(a, msg, "/goal "+objective); err != nil {
		t.Fatalf("handleCommand(/goal objective) error = %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"thread/goal/get", "thread/goal/set"}) {
		t.Fatalf("goal set calls = %#v", calls)
	}
	if len(setParams) != 1 || setParams[0].Objective == nil || *setParams[0].Objective != objective {
		t.Fatalf("goal set params = %#v, want raw objective %q", setParams, objective)
	}
	if setParams[0].Status == nil || *setParams[0].Status != codexrpc.ThreadGoalStatusActive {
		t.Fatalf("goal set status = %#v, want active", setParams[0].Status)
	}
	if setParams[0].TokenBudget != nil {
		t.Fatalf("plain /goal objective should not parse token budget, got %#v", setParams[0].TokenBudget)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("/goal objective should not send a Goal active card, replyCards=%d", len(ff.replyCards))
	}
	if len(ff.replyTexts) != 1 || !strings.Contains(ff.replyTexts[0], "已设置 goal") {
		t.Fatalf("/goal objective replyTexts = %#v, want concise text ack", ff.replyTexts)
	}
	if anchor, ok := goalTrackerForApp(a).anchor("thread-1"); !ok || anchor.MessageID != "" {
		t.Fatalf("goal management card should not become a turn reply anchor, got %+v / %v", anchor, ok)
	}

	ff.sendCardIDs = []string{"goal-turn-root-1"}
	handleNotification(a, "turn/started", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-goal-first"}}`))
	foundSessionKey, sub := newSubmissionQueueServiceFromApp(a).FindSubmissionByTurn("thread-1", "turn-goal-first")
	if foundSessionKey == "" || sub == nil {
		t.Fatalf("first goal turn binding = %q / %+v, want synthetic submission", foundSessionKey, sub)
	}
	if sub.TriggerMessageID != "goal-turn-root-1" {
		t.Fatalf("first goal turn trigger = %q, want fresh outbound root", sub.TriggerMessageID)
	}
	if len(ff.sendCards) != 1 || len(ff.sendCardChatIDs) != 1 || ff.sendCardChatIDs[0] != "chat-1" {
		t.Fatalf("first goal turn outbound cards = %#v chats=%#v, want one outbound card to chat-1", ff.sendCards, ff.sendCardChatIDs)
	}
}

func TestCommandGoalWithoutCurrentGoalRendersCreateForm(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-goal", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := seedGoalTestSession(t, a, msg, "thread-1")

	var calls []string
	var setParams []codexrpc.ThreadGoalSetParams
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		calls = append(calls, method)
		switch method {
		case "thread/goal/get":
			out.(*codexrpc.ThreadGoalGetResponse).Goal = nil
		case "thread/goal/set":
			p := params.(codexrpc.ThreadGoalSetParams)
			setParams = append(setParams, p)
			objective := ""
			if p.Objective != nil {
				objective = *p.Objective
			}
			status := codexrpc.ThreadGoalStatusActive
			if p.Status != nil {
				status = *p.Status
			}
			out.(*codexrpc.ThreadGoalSetResponse).Goal = testThreadGoal(p.ThreadID, objective, status)
		default:
			t.Fatalf("unexpected Codex method: %s", method)
		}
		return nil
	}

	if err := handleCommand(a, msg, "/goal"); err != nil {
		t.Fatalf("handleCommand(/goal no current goal) error = %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"thread/goal/get"}) {
		t.Fatalf("/goal no current goal calls = %#v", calls)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("/goal no current goal reply cards = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "当前 thread 没有 goal") || !strings.Contains(body, "输入 objective") {
		t.Fatalf("/goal create form body = %q", body)
	}
	form := goalFormForTest(t, ff.replyCards[0])
	inputs := goalFormInputsForTest(t, form)
	if objectiveInput := inputs["objective"]; objectiveInput == nil || objectiveInput["required"] != true {
		t.Fatalf("goal create form objective input = %#v", objectiveInput)
	}

	calls = nil
	resp, err := newGoalService(a).CompleteGoalEditSubmit(&feishu.CardAction{
		ActionValue: map[string]any{
			"session_key": sessionKey,
			"thread_id":   "thread-1",
			"status":      string(codexrpc.ThreadGoalStatusActive),
		},
		FormValue: map[string]any{
			"objective": "created from menu",
		},
		MessageID: "goal-create-card",
		ChatID:    "chat-1",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("CompleteGoalEditSubmit(create) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("create submit response = %#v", resp)
	}
	if resp.Card == nil {
		t.Fatalf("create submit should compact the form card, got %#v", resp)
	}
	if body := cardMarkdownContent(t, resp.Card.Data.(map[string]any)); !strings.Contains(body, "created from menu") || strings.Contains(body, "status:") {
		t.Fatalf("create submit card body = %q, want compact goal ack", body)
	}
	if !reflect.DeepEqual(calls, []string{"thread/goal/set"}) {
		t.Fatalf("create submit calls = %#v", calls)
	}
	if len(setParams) != 1 || setParams[0].Objective == nil || *setParams[0].Objective != "created from menu" {
		t.Fatalf("create submit set params = %#v", setParams)
	}
	if setParams[0].Status == nil || *setParams[0].Status != codexrpc.ThreadGoalStatusActive {
		t.Fatalf("create submit status = %#v", setParams[0].Status)
	}
	if anchor, ok := goalTrackerForApp(a).anchor("thread-1"); !ok || anchor.MessageID != "" {
		t.Fatalf("goal create card should not become a turn reply anchor, got %+v / %v", anchor, ok)
	}
}

func TestCommandGoalControlsValidateAndCallExpectedMethods(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-goal", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	seedGoalTestSession(t, a, msg, "thread-1")

	var calls []string
	var statuses []codexrpc.ThreadGoalStatus
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		calls = append(calls, method)
		switch method {
		case "thread/goal/set":
			p := params.(codexrpc.ThreadGoalSetParams)
			if p.ThreadID != "thread-1" {
				t.Fatalf("set threadId = %q", p.ThreadID)
			}
			if p.Objective != nil {
				t.Fatalf("status command should not send objective: %#v", p.Objective)
			}
			if p.Status == nil {
				t.Fatalf("status command missing status")
			}
			statuses = append(statuses, *p.Status)
			out.(*codexrpc.ThreadGoalSetResponse).Goal = testThreadGoal("thread-1", "current goal", *p.Status)
		case "thread/goal/clear":
			raw, _ := params.(map[string]any)
			if raw["threadId"] != "thread-1" {
				t.Fatalf("clear params = %#v", params)
			}
			out.(*codexrpc.ThreadGoalClearResponse).Cleared = true
		default:
			t.Fatalf("unexpected Codex method: %s", method)
		}
		return nil
	}

	for _, raw := range []string{"/goal pause", "/goal resume", "/goal clear"} {
		if err := handleCommand(a, msg, raw); err != nil {
			t.Fatalf("handleCommand(%q) error = %v", raw, err)
		}
	}
	if !reflect.DeepEqual(calls, []string{"thread/goal/set", "thread/goal/set", "thread/goal/clear"}) {
		t.Fatalf("control calls = %#v", calls)
	}
	if !reflect.DeepEqual(statuses, []codexrpc.ThreadGoalStatus{codexrpc.ThreadGoalStatusPaused, codexrpc.ThreadGoalStatusActive}) {
		t.Fatalf("status updates = %#v", statuses)
	}
	if len(ff.replyCards) != 3 {
		t.Fatalf("reply cards = %d, want 3", len(ff.replyCards))
	}

	calls = nil
	err := handleCommand(a, msg, "/goal "+strings.Repeat("x", goalMaxObjectiveRunes+1))
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("overlong /goal error = %v, want too long", err)
	}
	if len(calls) != 0 {
		t.Fatalf("overlong /goal should not call Codex, got %#v", calls)
	}
}

func TestCommandGoalExistingUnfinishedGoalRequiresConfirmation(t *testing.T) {
	a, ff, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-goal", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	seedGoalTestSession(t, a, msg, "thread-1")
	existing := testThreadGoal("thread-1", "finish current migration", codexrpc.ThreadGoalStatusActive)

	var calls []string
	fc.callHook = func(_ context.Context, method string, _ any, out any) error {
		calls = append(calls, method)
		if method != "thread/goal/get" {
			t.Fatalf("replacement prompt should not call %s before confirmation", method)
		}
		out.(*codexrpc.ThreadGoalGetResponse).Goal = &existing
		return nil
	}

	if err := handleCommand(a, msg, "/goal ship the new workflow"); err != nil {
		t.Fatalf("handleCommand(/goal replace) error = %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"thread/goal/get"}) {
		t.Fatalf("replacement calls = %#v", calls)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply cards = %d, want 1", len(ff.replyCards))
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "当前 thread 已有未完成 goal") || !strings.Contains(body, "finish current migration") || !strings.Contains(body, "ship the new workflow") {
		t.Fatalf("replace confirmation body = %q", body)
	}
	labels := cardButtonLabelsByAction(ff.replyCards[0])
	if labels["goal.replace.confirm"] == "" || labels["goal.replace.cancel"] == "" {
		t.Fatalf("replace confirmation buttons = %#v", labels)
	}
}

func TestGoalActionsReplaceConfirmAndEditSubmit(t *testing.T) {
	a, _, fc := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "m-goal", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := seedGoalTestSession(t, a, msg, "thread-1")

	var calls []string
	var setParams []codexrpc.ThreadGoalSetParams
	fc.callHook = func(_ context.Context, method string, params any, out any) error {
		calls = append(calls, method)
		switch method {
		case "thread/goal/clear":
			out.(*codexrpc.ThreadGoalClearResponse).Cleared = true
		case "thread/goal/set":
			p := params.(codexrpc.ThreadGoalSetParams)
			setParams = append(setParams, p)
			status := codexrpc.ThreadGoalStatusActive
			if p.Status != nil {
				status = *p.Status
			}
			objective := ""
			if p.Objective != nil {
				objective = *p.Objective
			}
			out.(*codexrpc.ThreadGoalSetResponse).Goal = testThreadGoal(p.ThreadID, objective, status)
		default:
			t.Fatalf("unexpected Codex method: %s", method)
		}
		return nil
	}

	resp, err := newGoalService(a).CompleteGoalReplaceConfirm(&feishu.CardAction{
		ActionValue: map[string]any{
			"session_key": sessionKey,
			"thread_id":   "thread-1",
			"objective":   "new confirmed goal",
		},
		MessageID: "goal-card",
		ChatID:    "chat-1",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("CompleteGoalReplaceConfirm() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("replace confirm response = %#v", resp)
	}
	if resp.Card == nil {
		t.Fatalf("replace confirm should compact the card, got %#v", resp)
	}
	if body := cardMarkdownContent(t, resp.Card.Data.(map[string]any)); !strings.Contains(body, "new confirmed goal") || strings.Contains(body, "status:") {
		t.Fatalf("replace confirm card body = %q, want compact goal ack", body)
	}
	if !reflect.DeepEqual(calls, []string{"thread/goal/clear", "thread/goal/set"}) {
		t.Fatalf("replace confirm calls = %#v", calls)
	}
	if len(setParams) != 1 || setParams[0].Objective == nil || *setParams[0].Objective != "new confirmed goal" {
		t.Fatalf("replace confirm set params = %#v", setParams)
	}

	calls = nil
	setParams = nil
	resp, err = newGoalService(a).CompleteGoalEditSubmit(&feishu.CardAction{
		ActionValue: map[string]any{
			"session_key":  sessionKey,
			"thread_id":    "thread-1",
			"status":       string(codexrpc.ThreadGoalStatusPaused),
			"token_budget": "12345",
		},
		FormValue: map[string]any{
			"objective": "edited goal",
		},
		MessageID: "goal-card",
		ChatID:    "chat-1",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("CompleteGoalEditSubmit() error = %v", err)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("edit submit response = %#v", resp)
	}
	if resp.Card == nil {
		t.Fatalf("edit submit should compact the card, got %#v", resp)
	}
	if body := cardMarkdownContent(t, resp.Card.Data.(map[string]any)); !strings.Contains(body, "edited goal") || strings.Contains(body, "status:") {
		t.Fatalf("edit submit card body = %q, want compact goal ack", body)
	}
	if !reflect.DeepEqual(calls, []string{"thread/goal/set"}) {
		t.Fatalf("edit submit calls = %#v", calls)
	}
	if len(setParams) != 1 || setParams[0].Objective == nil || *setParams[0].Objective != "edited goal" {
		t.Fatalf("edit submit set params = %#v", setParams)
	}
	if setParams[0].Status == nil || *setParams[0].Status != codexrpc.ThreadGoalStatusPaused {
		t.Fatalf("edit submit status = %#v", setParams[0].Status)
	}
	if setParams[0].TokenBudget == nil || setParams[0].TokenBudget.Value == nil || *setParams[0].TokenBudget.Value != 12345 {
		t.Fatalf("edit submit token budget = %#v", setParams[0].TokenBudget)
	}
}

func TestGoalNotificationsBindActiveGoalContinuationTurn(t *testing.T) {
	a, ff, _ := newTestApp(t)
	msg := &feishu.InboundMessage{MessageID: "root-msg", ChatID: "chat-1", ChatType: "p2p", UserID: "user-1"}
	sessionKey := seedGoalTestSession(t, a, msg, "thread-1")
	ff.sendCardIDs = []string{"goal-turn-root-1", "goal-turn-root-2"}

	handleNotification(a, "thread/goal/updated", json.RawMessage(`{"threadId":"thread-1","goal":{"threadId":"thread-1","objective":"keep going","status":"active","tokenBudget":null,"tokensUsed":0,"timeUsedSeconds":0,"createdAt":1,"updatedAt":2}}`))
	if goal, ok := goalTrackerForApp(a).activeGoal("thread-1"); !ok || goal.Objective != "keep going" {
		t.Fatalf("active goal after notification = %+v / %v", goal, ok)
	}

	handleNotification(a, "turn/started", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-goal"}}`))
	foundSessionKey, sub := newSubmissionQueueServiceFromApp(a).FindSubmissionByTurn("thread-1", "turn-goal")
	if foundSessionKey != sessionKey || sub == nil {
		t.Fatalf("goal continuation binding = %q / %+v, want %q", foundSessionKey, sub, sessionKey)
	}
	if sub.Kind != goalSubmissionKind || sub.InputText != goalContinuationInputText || sub.TriggerMessageID != "goal-turn-root-1" || sub.Status != state.SubmissionStatusRunning.String() {
		t.Fatalf("goal continuation submission = %+v", sub)
	}
	if len(sub.SourceRootMessageIDs) != 1 || sub.SourceRootMessageIDs[0] != "goal-turn-root-1" {
		t.Fatalf("goal continuation source roots = %#v, want outbound root only", sub.SourceRootMessageIDs)
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil || sess.ActiveTurnID != "turn-goal" || sess.ActiveSubmissionID != sub.ID || sess.Status != state.SessionStatusTurnInProgress.String() {
		t.Fatalf("session after goal continuation = %+v", sess)
	}
	if len(ff.sendCards) != 1 || len(ff.sendCardChatIDs) != 1 || ff.sendCardChatIDs[0] != "chat-1" {
		t.Fatalf("goal continuation outbound cards = %#v chats=%#v, want one card to chat-1", ff.sendCards, ff.sendCardChatIDs)
	}
	if title := cardHeaderTitle(t, ff.sendCards[0]); !strings.Contains(title, "Turn #1") || !strings.Contains(title, "keep going") {
		t.Fatalf("goal continuation anchor title = %q, want turn counter and objective", title)
	}
	if body := cardMarkdownContent(t, ff.sendCards[0]); !strings.Contains(body, "time: `0s`") || !strings.Contains(body, "tokens: `0`") || strings.Contains(body, "Turn #") || strings.Contains(body, "objective:") || strings.Contains(body, "turn:") || strings.Contains(body, "thread:") || strings.Contains(body, "status:") {
		t.Fatalf("goal continuation anchor body = %q, want compact time/tokens only", body)
	}
	if buttons := cardButtonsForTest(ff.sendCards[0]); len(buttons) != 0 {
		t.Fatalf("goal continuation buttons = %#v, want none", buttons)
	}

	if err := a.store.UpdateSubmission(sub.ID, func(current *state.Submission) {
		current.Status = state.SubmissionStatusCompleted.String()
	}); err != nil {
		t.Fatalf("UpdateSubmission(first goal continuation) error = %v", err)
	}
	if _, err := a.store.UpdateSession(sessionKey, func(current *state.Session) {
		sessionctx.ResetActiveOperations(current)
		current.Status = state.SessionStatusIdle.String()
	}); err != nil {
		t.Fatalf("UpdateSession(first goal continuation complete) error = %v", err)
	}

	handleNotification(a, "turn/started", json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-goal-2"}}`))
	_, sub = newSubmissionQueueServiceFromApp(a).FindSubmissionByTurn("thread-1", "turn-goal-2")
	if sub == nil || sub.TriggerMessageID != "goal-turn-root-2" {
		t.Fatalf("second goal continuation submission = %+v, want fresh outbound root", sub)
	}
	if len(sub.SourceRootMessageIDs) != 1 || sub.SourceRootMessageIDs[0] != "goal-turn-root-2" {
		t.Fatalf("second goal continuation source roots = %#v, want second outbound root only", sub.SourceRootMessageIDs)
	}
	if len(ff.sendCards) != 2 || len(ff.sendCardChatIDs) != 2 || ff.sendCardChatIDs[1] != "chat-1" {
		t.Fatalf("second goal continuation outbound cards = %#v chats=%#v, want another card to chat-1", ff.sendCards, ff.sendCardChatIDs)
	}
	if title := cardHeaderTitle(t, ff.sendCards[1]); !strings.Contains(title, "Turn #2") || !strings.Contains(title, "keep going") {
		t.Fatalf("second goal continuation anchor title = %q, want turn counter and objective", title)
	}
	if body := cardMarkdownContent(t, ff.sendCards[1]); strings.Contains(body, "Turn #") || strings.Contains(body, "objective:") || strings.Contains(body, "turn:") || strings.Contains(body, "thread:") {
		t.Fatalf("second goal continuation anchor body = %q, want compact time/tokens only", body)
	}
	if buttons := cardButtonsForTest(ff.sendCards[1]); len(buttons) != 0 {
		t.Fatalf("second goal continuation buttons = %#v, want none", buttons)
	}

	handleNotification(a, "thread/goal/cleared", json.RawMessage(`{"threadId":"thread-1"}`))
	if goal, ok := goalTrackerForApp(a).activeGoal("thread-1"); ok {
		t.Fatalf("goal should be cleared, got %+v", goal)
	}
}
