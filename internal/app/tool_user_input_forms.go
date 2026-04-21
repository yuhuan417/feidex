package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) sendUserInputFormCard(requestID json.RawMessage, payload toolUserInputPayload) {
	appState := a.appState()
	sessionKey, sub := a.findSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for request_user_input")
		return
	}
	requestKey := requestIDKey(requestID)
	card := renderToolUserInputFormCard(requestKey, payload, nil)
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		a.recordMessageLink(msgID, "user_input_card", sub, requestKey)
		_ = appState.savePending(&state.PendingRequest{
			ID:           requestKey,
			RequestIDRaw: requestIDStored(requestID),
			Kind:         "tool_request_user_input_form",
			SessionKey:   sessionKey,
			ThreadID:     payload.ThreadID,
			TurnID:       payload.TurnID,
			ItemID:       payload.ItemID,
			OwnerUserID:  sub.UserID,
			FeishuMsgID:  msgID,
			PayloadJSON:  mustJSON(payload),
			Status:       "pending",
			CreatedAt:    time.Now().Unix(),
			ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
		})
		_ = appState.setSubmissionStatus(sub.ID, "waiting_user_input")
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func (a *App) completeToolUserInputText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	var payload toolUserInputPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return err
	}
	response, summary, err := parseToolUserInputResponse(strings.TrimSpace(msg.Text), payload)
	if err != nil {
		return err
	}
	if pendingBackend(a, pending) == backendClaude {
		answers, _, err := parseClaudeToolUserInputResponse(strings.TrimSpace(msg.Text), payload)
		if err != nil {
			return err
		}
		if err := a.claude.ResolveUserInput(pending.ID, answers); err != nil {
			return err
		}
		_ = a.appState().updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
		a.resumeSubmissionAfterRequest(pending)
		if pending.FeishuMsgID != "" {
			_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("已提交", "green", summary, nil))
		}
		return nil
	}
	if err := a.codex.Reply(pendingRequestIDRaw(pending), response); err != nil {
		return err
	}
	_ = a.markPendingRequestReplied(pending.ID)
	if pending.FeishuMsgID != "" {
		_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("已提交", "green", summary, nil))
	}
	return nil
}

func renderToolUserInputBody(payload toolUserInputPayload) string {
	lines := []string{"请补充以下输入。"}
	for _, q := range payload.Questions {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s (`%s`)", firstNonEmpty(strings.TrimSpace(q.Question), strings.TrimSpace(q.Header), strings.TrimSpace(q.ID)), q.ID))
		if len(q.Options) > 0 {
			opts := make([]string, 0, len(q.Options))
			for _, opt := range q.Options {
				opts = append(opts, opt.Label)
			}
			lines = append(lines, "可选值: "+strings.Join(opts, ", "))
			if q.IsOther {
				lines = append(lines, "也可填写其它值。")
			}
		}
		if q.IsSecret {
			lines = append(lines, "注意: 此答案会按敏感输入处理，不写普通日志。")
		}
	}
	if len(payload.Questions) > 0 {
		lines = append(lines, "", "表单中的每个输入框对应一道题。")
		lines = append(lines, "如果一题要填多个值，可用逗号分隔。")
	}
	return strings.Join(lines, "\n")
}

func renderToolUserInputFormCard(requestID string, payload toolUserInputPayload, drafts map[string]string) map[string]any {
	card := newMarkdownBodyCard("需要补充输入", "orange")
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": renderToolUserInputBody(payload),
	})
	buttonRows := buildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text:  "提交",
			Type:  "primary",
			Name:  "user_input_submit",
			Value: map[string]any{"action": "user_input.answer", "request_id": strings.TrimSpace(requestID)},
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "user_input_cancel",
			Value: map[string]any{"action": "pending_form.cancel", "request_id": strings.TrimSpace(requestID)},
		},
	})
	for idx, row := range buttonRows {
		columns, _ := row["columns"].([]map[string]any)
		if len(columns) == 0 {
			continue
		}
		elements, _ := columns[0]["elements"].([]map[string]any)
		if len(elements) == 0 {
			continue
		}
		if idx == 0 {
			elements[0]["form_action_type"] = "submit"
		}
	}
	formElements := make([]map[string]any, 0, len(payload.Questions)+len(buttonRows))
	for _, q := range payload.Questions {
		input := map[string]any{
			"tag":         "input",
			"name":        q.ID,
			"required":    true,
			"placeholder": map[string]any{"tag": "plain_text", "content": toolUserInputQuestionPlaceholder(q)},
		}
		if value := strings.TrimSpace(drafts[q.ID]); value != "" {
			input["default_value"] = value
		}
		formElements = append(formElements, input)
	}
	formElements = append(formElements, buttonRows...)
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag":                "form",
		"name":               "tool_user_input_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements":           formElements,
	})
	return card
}

func toolUserInputQuestionPlaceholder(q toolUserInputQuestion) string {
	title := firstNonEmpty(strings.TrimSpace(q.Question), strings.TrimSpace(q.Header), strings.TrimSpace(q.ID), "请输入答案")
	if len(q.Options) == 0 {
		return title
	}
	opts := make([]string, 0, len(q.Options))
	for _, opt := range q.Options {
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			continue
		}
		opts = append(opts, label)
		if len(opts) >= 3 {
			break
		}
	}
	if len(opts) == 0 {
		return title
	}
	suffix := "例如: " + strings.Join(opts, ", ")
	if q.IsOther {
		suffix += "，也可填写其它值"
	}
	return title + " | " + suffix
}

func parseToolUserInputResponse(text string, payload toolUserInputPayload) (map[string]any, string, error) {
	answerMap := parseStructuredLines(text)
	selections := make(map[string]string, len(payload.Questions))
	for _, q := range payload.Questions {
		raw := strings.TrimSpace(answerMap[q.ID])
		if raw == "" && len(payload.Questions) == 1 {
			raw = text
		}
		selections[q.ID] = raw
	}
	return buildToolUserInputResponseFromSelections(payload, selections)
}

func parseQuestionAnswers(raw string, q toolUserInputQuestion) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("answer is required")
	}
	if len(q.Options) == 0 {
		return []string{raw}, nil
	}
	parts := splitAnswerParts(raw)
	allowed := map[string]string{}
	for _, opt := range q.Options {
		allowed[strings.ToLower(strings.TrimSpace(opt.Label))] = opt.Label
	}
	var answers []string
	for _, part := range parts {
		if matched, ok := allowed[strings.ToLower(part)]; ok {
			answers = append(answers, matched)
			continue
		}
		if q.IsOther {
			answers = append(answers, part)
			continue
		}
		return nil, fmt.Errorf("unsupported option %q", part)
	}
	if len(answers) == 0 {
		return nil, fmt.Errorf("answer is required")
	}
	return answers, nil
}

func splitAnswerParts(raw string) []string {
	raw = strings.ReplaceAll(raw, "\n", ",")
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func summarizeAnswers(answers []string, secret bool) string {
	if secret {
		return "[redacted]"
	}
	return strings.Join(answers, ", ")
}

func buildToolUserInputResponseFromSelections(payload toolUserInputPayload, selections map[string]string) (map[string]any, string, error) {
	result := map[string]any{"answers": map[string]any{}}
	summaryLines := make([]string, 0, len(payload.Questions))
	for _, q := range payload.Questions {
		raw := strings.TrimSpace(selections[q.ID])
		answers, err := parseQuestionAnswers(raw, q)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", q.ID, err)
		}
		result["answers"].(map[string]any)[q.ID] = map[string]any{"answers": answers}
		summaryLines = append(summaryLines, fmt.Sprintf("`%s`: %s", q.ID, summarizeAnswers(answers, q.IsSecret)))
	}
	return result, strings.Join(summaryLines, "\n"), nil
}

func toolUserInputSelectionsFromFormValues(payload toolUserInputPayload, values map[string]any) map[string]string {
	selections := make(map[string]string, len(payload.Questions))
	for _, q := range payload.Questions {
		if value, ok := toolUserInputSelectionValue(values, q.ID); ok {
			selections[q.ID] = value
		}
	}
	return selections
}

func toolUserInputSelectionValue(values map[string]any, key string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	raw, ok := values[key]
	if !ok {
		return "", false
	}
	return normalizeToolUserInputSelection(raw)
}

func normalizeToolUserInputSelection(raw any) (string, bool) {
	switch v := raw.(type) {
	case nil:
		return "", false
	case string:
		return strings.TrimSpace(v), true
	case []string:
		return strings.TrimSpace(strings.Join(v, ", ")), true
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			part, ok := normalizeToolUserInputSelection(item)
			if ok && part != "" {
				parts = append(parts, part)
			}
		}
		return strings.TrimSpace(strings.Join(parts, ", ")), true
	case map[string]any:
		if value, ok := v["value"]; ok {
			return normalizeToolUserInputSelection(value)
		}
	}
	return strings.TrimSpace(fmt.Sprint(raw)), true
}
