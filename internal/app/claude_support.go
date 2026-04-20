package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func claudeRequestIDStored(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ""
	}
	return mustJSON(requestID)
}

func (a *App) sendClaudeApprovalCardWithPayload(kind, requestID, sessionKey string, sub *state.Submission, threadID, turnID, itemID, body string, requestPayload map[string]any) error {
	if a == nil || a.feishu == nil || sub == nil {
		return fmt.Errorf("claude approval delivery unavailable")
	}
	appState := a.appState()
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}

	title := "等待审批"
	buttons := approvalButtons(kind, requestKey, requestPayload)
	payload := map[string]any{}
	if strings.TrimSpace(body) != "" {
		payload["body"] = body
	}
	if len(requestPayload) > 0 {
		payload["request"] = requestPayload
	}
	if strings.TrimSpace(kind) == "permissions" {
		title = "权限请求"
		buttons = []feishu.Button{
			{Text: "本次允许", Type: "primary", Value: map[string]any{"action": "approval.permissions.accept_turn", "request_id": requestKey}},
			{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval.permissions.accept_session", "request_id": requestKey}},
			{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval.permissions.decline", "request_id": requestKey}},
		}
		if permissions, ok := requestPayload["permissions"]; ok {
			payload["permissions"] = permissions
		}
	}

	card := a.renderApprovalCard(sessionKey, sub, title, "orange", strings.TrimSpace(body), buttons)
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err != nil {
		return err
	}
	a.recordMessageLink(msgID, "approval_card", sub, requestKey)
	_ = appState.savePending(&state.PendingRequest{
		ID:           requestKey,
		RequestIDRaw: claudeRequestIDStored(requestKey),
		Backend:      backendClaude,
		Kind:         strings.TrimSpace(kind),
		SessionKey:   strings.TrimSpace(sessionKey),
		ThreadID:     strings.TrimSpace(threadID),
		TurnID:       strings.TrimSpace(turnID),
		ItemID:       strings.TrimSpace(itemID),
		OwnerUserID:  strings.TrimSpace(sub.UserID),
		FeishuMsgID:  msgID,
		PayloadJSON:  mustJSON(payload),
		Status:       "pending",
		CreatedAt:    time.Now().Unix(),
		ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
	})
	_ = appState.setSubmissionStatus(sub.ID, "waiting_approval")
	return nil
}

func (a *App) sendClaudeUserInputCard(requestID, sessionKey string, sub *state.Submission, payload toolUserInputPayload) error {
	if a == nil || a.feishu == nil || sub == nil || len(payload.Questions) == 0 {
		return fmt.Errorf("claude question delivery unavailable")
	}
	appState := a.appState()
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
	card := a.feishu.SimpleStatusCard("需要补充输入", "orange", q.Question, buttons)
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err != nil {
		return err
	}
	a.recordMessageLink(msgID, "user_input_card", sub, requestKey)
	_ = appState.savePending(&state.PendingRequest{
		ID:           requestKey,
		RequestIDRaw: claudeRequestIDStored(requestKey),
		Backend:      backendClaude,
		Kind:         "tool_request_user_input",
		SessionKey:   strings.TrimSpace(sessionKey),
		ThreadID:     payload.ThreadID,
		TurnID:       payload.TurnID,
		ItemID:       payload.ItemID,
		OwnerUserID:  strings.TrimSpace(sub.UserID),
		FeishuMsgID:  msgID,
		PayloadJSON:  mustJSON(payload),
		Status:       "pending",
		CreatedAt:    time.Now().Unix(),
		ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
	})
	_ = appState.setSubmissionStatus(sub.ID, "waiting_user_input")
	return nil
}

func (a *App) sendClaudeUserInputFormCard(requestID, sessionKey string, sub *state.Submission, payload toolUserInputPayload) error {
	if a == nil || a.feishu == nil || sub == nil {
		return fmt.Errorf("claude question delivery unavailable")
	}
	appState := a.appState()
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}
	card := a.feishu.SimpleStatusCard("需要补充输入", "orange", renderToolUserInputBody(payload), []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestKey}},
	})
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err != nil {
		return err
	}
	a.recordMessageLink(msgID, "user_input_card", sub, requestKey)
	_ = appState.savePending(&state.PendingRequest{
		ID:           requestKey,
		RequestIDRaw: claudeRequestIDStored(requestKey),
		Backend:      backendClaude,
		Kind:         "tool_request_user_input_form",
		SessionKey:   strings.TrimSpace(sessionKey),
		ThreadID:     payload.ThreadID,
		TurnID:       payload.TurnID,
		ItemID:       payload.ItemID,
		OwnerUserID:  strings.TrimSpace(sub.UserID),
		FeishuMsgID:  msgID,
		PayloadJSON:  mustJSON(payload),
		Status:       "pending",
		CreatedAt:    time.Now().Unix(),
		ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
	})
	_ = appState.setSubmissionStatus(sub.ID, "waiting_user_input")
	return nil
}

func (a *App) sendClaudePlanModeCard(requestID, sessionKey string, sub *state.Submission, threadID, turnID, body string) error {
	if a == nil || a.feishu == nil || sub == nil {
		return fmt.Errorf("claude plan confirmation unavailable")
	}
	appState := a.appState()
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}
	card := a.feishu.SimpleStatusCard("Claude 计划确认", "orange", strings.TrimSpace(body), []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestKey}},
	})
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err != nil {
		return err
	}
	a.recordMessageLink(msgID, "claude_plan_card", sub, requestKey)
	_ = appState.savePending(&state.PendingRequest{
		ID:           requestKey,
		RequestIDRaw: claudeRequestIDStored(requestKey),
		Backend:      backendClaude,
		Kind:         claudePlanModePendingKind,
		SessionKey:   strings.TrimSpace(sessionKey),
		ThreadID:     strings.TrimSpace(threadID),
		TurnID:       strings.TrimSpace(turnID),
		ItemID:       requestKey,
		OwnerUserID:  strings.TrimSpace(sub.UserID),
		FeishuMsgID:  msgID,
		PayloadJSON:  mustJSON(map[string]any{"body": strings.TrimSpace(body)}),
		Status:       "pending",
		CreatedAt:    time.Now().Unix(),
		ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
	})
	_ = appState.setSubmissionStatus(sub.ID, "waiting_user_input")
	return nil
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

func (a *App) completeClaudePlanModeText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	if msg == nil || pending == nil {
		return nil
	}
	feedback := strings.TrimSpace(msg.Text)
	if feedback == "" {
		return fmt.Errorf("反馈不能为空")
	}
	if err := a.claude.ResolvePlanFeedback(pending.ID, feedback); err != nil {
		return err
	}
	_ = a.appState().updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	a.resumeSubmissionAfterRequest(pending)
	if pending.FeishuMsgID != "" {
		_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("计划反馈已提交", "green", claudePlanSubmittedBody(pending, feedback), nil))
	}
	return nil
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

func (a *App) claudeUnsupportedCommand(raw string) error {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return nil
	}
	switch fields[0] {
	case "/history", "/skills", "/model", "/review", "/compact", "/fork", "/fast":
		return backendUnsupportedError(fields[0])
	case "/thread":
		if len(fields) <= 1 {
			return nil
		}
		switch strings.TrimSpace(fields[1]) {
		case "new":
			return nil
		default:
			return backendUnsupportedError("/thread " + strings.TrimSpace(fields[1]))
		}
	}
	return nil
}

func (a *App) renderClaudeThreadsCard(sessionKey string, sess *state.Session, ws *config.Workspace) map[string]any {
	workspaceID := "-"
	if ws != nil {
		workspaceID = firstNonEmpty(strings.TrimSpace(ws.ID), workspaceID)
	}
	threadID := "-"
	if sess != nil && strings.TrimSpace(sess.ActiveThreadID) != "" {
		threadID = strings.TrimSpace(sess.ActiveThreadID)
	}
	lines := []string{
		"当前 backend: `claude`",
		"当前工作区: `" + workspaceID + "`",
		"当前 session: `" + threadID + "`",
		"",
		"Claude core 目前支持继续当前会话与新建会话。",
		"暂不支持 `/thread list`、`/thread resume`、`/thread fork`、`/thread sandbox`、`/thread policy`。",
	}
	buttons := []feishu.Button{
		{
			Text: commandLabel("新建线程", "/thread new"),
			Type: "default",
			Value: map[string]any{
				"action":        "menu.new",
				"session_key":   sessionKey,
				"parent_action": "menu.thread",
			},
		},
		{
			Text: "返回上一级",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.root",
				"session_key": sessionKey,
			},
		},
	}
	return a.feishu.SimpleStatusCard("线程管理", "blue", menuCardBody("menu.thread", strings.Join(lines, "\n")), buttons)
}
