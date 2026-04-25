package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func claudeRequestIDStored(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ""
	}
	return mustJSON(requestID)
}

func sendClaudeApprovalCardWithPayload(a *App, kind, requestID, sessionKey string, sub *state.Submission, threadID, turnID, itemID, body string, requestPayload map[string]any, sessionActionLabel string) error {
	if a == nil || a.feishu == nil || sub == nil {
		return fmt.Errorf("claude approval delivery unavailable")
	}
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}

	title := "等待审批"
	buttons := claudeApprovalButtons(kind, requestKey, sessionActionLabel)
	payload := map[string]any{}
	if strings.TrimSpace(body) != "" {
		payload["body"] = body
	}
	if len(requestPayload) > 0 {
		payload["request"] = requestPayload
	}
	if strings.TrimSpace(sessionActionLabel) != "" {
		payload["session_action_label"] = strings.TrimSpace(sessionActionLabel)
	}
	if strings.TrimSpace(kind) == "permissions" {
		title = "权限请求"
		if permissions, ok := requestPayload["permissions"]; ok {
			payload["permissions"] = permissions
		}
	}

	card := renderApprovalCard(a, sessionKey, sub, title, "orange", strings.TrimSpace(body), buttons)
	return deliverPendingCard(a, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: claudeRequestIDStored(requestKey),
		backend:         backendClaude,
		kind:            strings.TrimSpace(kind),
		sessionKey:      strings.TrimSpace(sessionKey),
		threadID:        strings.TrimSpace(threadID),
		turnID:          strings.TrimSpace(turnID),
		itemID:          strings.TrimSpace(itemID),
		ownerUserID:     strings.TrimSpace(sub.UserID),
		payloadJSON:     mustJSON(payload),
		waitingStatus:   "waiting_approval",
		linkKind:        "approval_card",
	})
}

func claudeApprovalButtons(kind, requestKey, sessionActionLabel string) []feishu.Button {
	sessionActionLabel = strings.TrimSpace(sessionActionLabel)
	switch strings.TrimSpace(kind) {
	case "command":
		buttons := []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval.command.accept", "request_id": requestKey}},
		}
		if sessionActionLabel != "" {
			buttons = append(buttons, feishu.Button{Text: sessionActionLabel, Type: "default", Value: map[string]any{"action": "approval.command.accept_session", "request_id": requestKey}})
		}
		buttons = append(buttons,
			feishu.Button{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval.command.decline", "request_id": requestKey}},
			feishu.Button{Text: "拒绝并中断", Type: "danger", Value: map[string]any{"action": "approval.command.cancel", "request_id": requestKey}},
		)
		return buttons
	case "file":
		buttons := []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval.file.accept", "request_id": requestKey}},
		}
		if sessionActionLabel != "" {
			buttons = append(buttons, feishu.Button{Text: sessionActionLabel, Type: "default", Value: map[string]any{"action": "approval.file.accept_session", "request_id": requestKey}})
		}
		buttons = append(buttons,
			feishu.Button{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval.file.decline", "request_id": requestKey}},
			feishu.Button{Text: "拒绝并中断", Type: "danger", Value: map[string]any{"action": "approval.file.cancel", "request_id": requestKey}},
		)
		return buttons
	case "permissions":
		buttons := []feishu.Button{
			{Text: "本次允许", Type: "primary", Value: map[string]any{"action": "approval.permissions.accept_turn", "request_id": requestKey}},
		}
		if sessionActionLabel != "" {
			buttons = append(buttons, feishu.Button{Text: sessionActionLabel, Type: "default", Value: map[string]any{"action": "approval.permissions.accept_session", "request_id": requestKey}})
		}
		buttons = append(buttons, feishu.Button{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval.permissions.decline", "request_id": requestKey}})
		return buttons
	default:
		buttons := []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval." + kind + ".accept", "request_id": requestKey}},
		}
		if sessionActionLabel != "" {
			buttons = append(buttons, feishu.Button{Text: sessionActionLabel, Type: "default", Value: map[string]any{"action": "approval." + kind + ".accept_session", "request_id": requestKey}})
		}
		buttons = append(buttons, feishu.Button{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval." + kind + ".decline", "request_id": requestKey}})
		return buttons
	}
}

func safeClaudeSessionPermissionUpdates(suggestions []map[string]any) []map[string]any {
	if len(suggestions) == 0 {
		return nil
	}
	updates := make([]map[string]any, 0, len(suggestions))
	for _, suggestion := range suggestions {
		normalized, ok := normalizeClaudeSessionPermissionUpdate(suggestion)
		if ok {
			updates = append(updates, normalized)
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return updates
}

func normalizeClaudeSessionPermissionUpdate(update map[string]any) (map[string]any, bool) {
	if len(update) == 0 {
		return nil, false
	}
	if strings.TrimSpace(stringValue(update["destination"])) != "session" {
		return nil, false
	}
	switch strings.TrimSpace(stringValue(update["type"])) {
	case "setMode":
		mode := normalizeClaudePermissionModeValue(stringValue(update["mode"]))
		switch mode {
		case string(claudePermissionModeDefault), string(claudePermissionModeAcceptEdits), string(claudePermissionModeBypass):
		default:
			return nil, false
		}
		out := copyPermissionUpdates([]map[string]any{update})
		out[0]["mode"] = mode
		return out[0], true
	case "addRules":
		if firstNonEmptyValue(update["rules"], update["rule"]) == nil {
			return nil, false
		}
		out := copyPermissionUpdates([]map[string]any{update})
		return out[0], true
	default:
		return nil, false
	}
}

func describeClaudeSessionPermissionUpdates(updates []map[string]any) string {
	if len(updates) == 0 {
		return ""
	}
	if len(updates) == 1 {
		update := updates[0]
		switch strings.TrimSpace(stringValue(update["type"])) {
		case "setMode":
			mode := normalizeClaudePermissionModeValue(stringValue(update["mode"]))
			if mode != "" {
				return "切到 `" + mode + "`（当前会话）"
			}
		case "addRules":
			return "当前会话允许同类操作"
		}
	}
	return "应用建议（当前会话）"
}

func sendClaudeUserInputCard(a *App, requestID, sessionKey string, sub *state.Submission, payload toolUserInputPayload) error {
	if a == nil || a.feishu == nil || sub == nil || len(payload.Questions) == 0 {
		return fmt.Errorf("claude question delivery unavailable")
	}
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}
	q := payload.Questions[0]
	buttons := make([]feishu.Button, 0, len(q.Options))
	for _, opt := range q.Options {
		buttons = append(buttons, feishu.Button{
			Text: opt.Label,
			Type: "default",
			Value: map[string]any{
				"action":      "user_input.answer",
				"request_id":  requestKey,
				"question_id": q.ID,
				"answer":      opt.Label,
			},
		})
	}
	card := a.feishu.SimpleStatusCard("需要补充输入", "orange", prependAttentionMentionMarkdown(q.Question, sub.UserID), buttons)
	return deliverPendingCard(a, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: claudeRequestIDStored(requestKey),
		backend:         backendClaude,
		kind:            "tool_request_user_input",
		sessionKey:      strings.TrimSpace(sessionKey),
		threadID:        payload.ThreadID,
		turnID:          payload.TurnID,
		itemID:          payload.ItemID,
		ownerUserID:     strings.TrimSpace(sub.UserID),
		payloadJSON:     mustJSON(payload),
		waitingStatus:   "waiting_user_input",
		linkKind:        "user_input_card",
	})
}

func sendClaudeUserInputFormCard(a *App, requestID, sessionKey string, sub *state.Submission, payload toolUserInputPayload) error {
	if a == nil || a.feishu == nil || sub == nil {
		return fmt.Errorf("claude question delivery unavailable")
	}
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}
	card := renderToolUserInputFormCard(requestKey, payload, toolUserInputFormDrafts{}, sub.UserID)
	return deliverPendingCard(a, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: claudeRequestIDStored(requestKey),
		backend:         backendClaude,
		kind:            "tool_request_user_input_form",
		sessionKey:      strings.TrimSpace(sessionKey),
		threadID:        payload.ThreadID,
		turnID:          payload.TurnID,
		itemID:          payload.ItemID,
		ownerUserID:     strings.TrimSpace(sub.UserID),
		payloadJSON:     mustJSON(payload),
		waitingStatus:   "waiting_user_input",
		linkKind:        "user_input_card",
	})
}

func sendClaudePlanModeCard(a *App, requestID, sessionKey string, sub *state.Submission, threadID, turnID, body string) error {
	if a == nil || a.feishu == nil || sub == nil {
		return fmt.Errorf("claude plan confirmation unavailable")
	}
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}
	card := a.feishu.SimpleStatusCard("Claude 计划确认", "orange", prependAttentionMentionMarkdown(strings.TrimSpace(body), sub.UserID), []feishu.Button{
		{Text: "批准", Type: "primary", Value: map[string]any{"action": "pending_form.plan_approve", "request_id": requestKey}},
		{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "pending_form.plan_reject", "request_id": requestKey}},
	})
	return deliverPendingCard(a, sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: claudeRequestIDStored(requestKey),
		backend:         backendClaude,
		kind:            claudePlanModePendingKind,
		sessionKey:      strings.TrimSpace(sessionKey),
		threadID:        strings.TrimSpace(threadID),
		turnID:          strings.TrimSpace(turnID),
		itemID:          requestKey,
		ownerUserID:     strings.TrimSpace(sub.UserID),
		payloadJSON:     mustJSON(map[string]any{"body": strings.TrimSpace(body)}),
		waitingStatus:   "waiting_user_input",
		linkKind:        "claude_plan_card",
	})
}

func claudeApprovalResolutionForAction(actionName string) (claudeApprovalResolution, string) {
	switch strings.TrimSpace(actionName) {
	case "approval.command.accept", "approval.file.accept", "approval.permissions.accept_turn":
		return claudeApprovalResolution{Behavior: "allow", Scope: "turn"}, ""
	case "approval.command.accept_session", "approval.file.accept_session", "approval.permissions.accept_session":
		return claudeApprovalResolution{Behavior: "allow", Scope: "session"}, ""
	case "approval.command.cancel", "approval.file.cancel":
		return claudeApprovalResolution{
			Behavior:  "deny",
			Message:   "Declined by user",
			Interrupt: true,
		}, ""
	case "approval.command.decline", "approval.file.decline", "approval.permissions.decline":
		return claudeApprovalResolution{
			Behavior: "deny",
			Message:  "Declined by user",
		}, ""
	default:
		return claudeApprovalResolution{}, "不支持的审批动作"
	}
}

func claudeAnswersFromSelections(payload toolUserInputPayload, selections map[string]string) (map[string]string, string, error) {
	answers := map[string]string{}
	summaryLines := make([]string, 0, len(selections))
	for _, q := range payload.Questions {
		raw := strings.TrimSpace(selections[q.ID])
		if raw == "" {
			continue
		}
		value, summary, err := claudeQuestionAnswer(raw, q)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", q.ID, err)
		}
		answers[q.Question] = value
		summaryLines = append(summaryLines, fmt.Sprintf("`%s`: %s", q.ID, summary))
	}
	if len(answers) == 0 {
		return nil, "", fmt.Errorf("answer is required")
	}
	return answers, strings.Join(summaryLines, "\n"), nil
}

func parseClaudeToolUserInputResponse(text string, payload toolUserInputPayload) (map[string]string, string, error) {
	answerMap := parseStructuredLines(text)
	selections := make(map[string]string, len(payload.Questions))
	for _, q := range payload.Questions {
		raw := strings.TrimSpace(answerMap[q.ID])
		if raw == "" && len(payload.Questions) == 1 {
			raw = text
		}
		selections[q.ID] = raw
	}
	return claudeAnswersFromSelections(payload, selections)
}

func claudeQuestionAnswer(raw string, q toolUserInputQuestion) (string, string, error) {
	answers, err := parseQuestionAnswers(raw, q)
	if err != nil {
		return "", "", err
	}
	return strings.Join(answers, ", "), summarizeAnswers(answers, q.IsSecret), nil
}

func (s pendingInputService) completeClaudePlanModeText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	if msg == nil || pending == nil {
		return nil
	}
	feedback := strings.TrimSpace(msg.Text)
	if feedback == "" {
		return fmt.Errorf("反馈不能为空")
	}
	if err := s.app.claude.ResolvePlanFeedback(pending.ID, feedback); err != nil {
		return err
	}
	_ = newRuntimeStateService(s.app).finalizePendingReply(pending)
	if pending.FeishuMsgID != "" {
		_ = s.app.feishu.PatchCard(context.Background(), pending.FeishuMsgID, s.app.feishu.SimpleStatusCard("计划反馈已提交", "green", claudePlanSubmittedBody(pending, feedback), nil))
	}
	return nil
}

func completePlanApprove(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState(a).pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	if err := a.claude.ResolvePlanFeedback(pending.ID, "Approve"); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"}}, nil
	}
	_ = newRuntimeStateService(a).finalizePendingReply(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已批准"},
		Card:  rawCard(a.feishu.SimpleStatusCard("计划已批准", "green", claudePlanSubmittedBody(pending, "Approve"), nil)),
	}, nil
}

func completePlanReject(a *App, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState(a).pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	adapter := serverRequestAdapterForPending(a, pending)
	if err := adapter.cancelPending(pending); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"}}, nil
	}
	_ = newRuntimeStateService(a).finalizePendingReply(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已拒绝"},
		Card:  rawCard(a.feishu.SimpleStatusCard("计划已拒绝", "grey", claudePlanCancelledBody(pending), nil)),
	}, nil
}

func claudePlanSubmittedBody(pending *state.PendingRequest, feedback string) string {
	lines := []string{"已提交给 Claude，等待继续处理。"}
	if strings.TrimSpace(feedback) != "" {
		lines = append(lines, "", "你的反馈：", strings.TrimSpace(feedback))
	}
	if original := claudePlanOriginalBody(pending); original != "" {
		lines = append(lines, "", "原计划：", original)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func claudePlanCancelledBody(pending *state.PendingRequest) string {
	lines := []string{"已取消本次计划确认。"}
	if original := claudePlanOriginalBody(pending); original != "" {
		lines = append(lines, "", "原计划：", original)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func claudePlanOriginalBody(pending *state.PendingRequest) string {
	if pending == nil || strings.TrimSpace(pending.PayloadJSON) == "" {
		return ""
	}
	var payload struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Body)
}
