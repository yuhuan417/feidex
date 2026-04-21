package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

type toolUserInputFormDrafts struct {
	Values map[string]string
	Multi  map[string][]string
}

func (a *App) sendUserInputFormCard(requestID json.RawMessage, payload toolUserInputPayload) {
	appState := a.appState()
	sessionKey, sub := a.findSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for request_user_input")
		return
	}
	requestKey := requestIDKey(requestID)
	card := renderToolUserInputFormCard(requestKey, payload, toolUserInputFormDrafts{})
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
			if q.MultiSelect {
				lines = append(lines, "这是多选题。")
			} else {
				lines = append(lines, "这是单选题。")
			}
			if q.IsOther {
				lines = append(lines, "也可填写其它值。")
			}
		}
		if q.IsSecret {
			lines = append(lines, "注意: 此答案会按敏感输入处理，不写普通日志。")
		}
	}
	if len(payload.Questions) > 0 {
		lines = append(lines, "", "单选题会显示下拉选择，多选题会显示可切换按钮。")
		lines = append(lines, "只有自由文本或其它值输入时，才需要手填文本。")
	}
	return strings.Join(lines, "\n")
}

func renderToolUserInputFormCard(requestID string, payload toolUserInputPayload, drafts toolUserInputFormDrafts) map[string]any {
	card := newMarkdownBodyCard("需要补充输入", "orange")
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": renderToolUserInputBody(payload),
	})
	formElements := make([]map[string]any, 0, len(payload.Questions)+4)
	for _, q := range payload.Questions {
		for _, elem := range renderToolUserInputQuestionElements(q, drafts, requestID) {
			formElements = append(formElements, elem)
		}
	}
	buttonRows := buildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text: "提交",
			Type: "primary",
			Name: "user_input_submit",
			Value: map[string]any{
				"action":       "user_input.answer",
				"request_id":   strings.TrimSpace(requestID),
				"multi_drafts": toolUserInputMultiDraftActionValue(drafts.Multi),
			},
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

func renderToolUserInputQuestionElements(q toolUserInputQuestion, drafts toolUserInputFormDrafts, requestID string) []map[string]any {
	elements := []map[string]any{
		{
			"tag": "markdown",
			"content": toolUserInputQuestionMarkdown(
				q,
				toolUserInputDraftValue(drafts, q.ID),
				toolUserInputMultiDraftValues(drafts, q.ID),
			),
		},
	}
	switch {
	case len(q.Options) == 0:
		elements = append(elements, buildToolUserInputTextInputElement(q, drafts))
	case q.MultiSelect:
		elements = append(elements, buildToolUserInputMultiSelectRows(q, drafts, requestID)...)
	default:
		elements = append(elements, buildToolUserInputSingleSelectElement(q, drafts))
	}
	if q.IsOther {
		elements = append(elements, buildToolUserInputOtherInputElement(q, drafts))
	}
	return elements
}

func toolUserInputQuestionMarkdown(q toolUserInputQuestion, draftValue string, selected []string) string {
	lines := []string{
		"**" + firstNonEmpty(strings.TrimSpace(q.Question), strings.TrimSpace(q.Header), strings.TrimSpace(q.ID)) + "**",
		"`" + strings.TrimSpace(q.ID) + "`",
	}
	if len(q.Options) > 0 {
		mode := "单选"
		if q.MultiSelect {
			mode = "多选"
		}
		lines = append(lines, mode+"题")
		if len(selected) > 0 {
			lines = append(lines, "当前已选: `"+inlineCodeText(strings.Join(selected, ", "))+"`")
		} else if q.MultiSelect {
			lines = append(lines, "当前已选: `-`")
		} else if strings.TrimSpace(draftValue) != "" {
			lines = append(lines, "当前选择: `"+inlineCodeText(strings.TrimSpace(draftValue))+"`")
		}
	}
	if q.IsOther {
		lines = append(lines, "可补充其它值。")
	}
	if q.IsSecret {
		lines = append(lines, "敏感输入会在展示中打码。")
	}
	return strings.Join(lines, "\n")
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

func buildToolUserInputTextInputElement(q toolUserInputQuestion, drafts toolUserInputFormDrafts) map[string]any {
	input := map[string]any{
		"tag":         "input",
		"name":        q.ID,
		"required":    true,
		"placeholder": map[string]any{"tag": "plain_text", "content": toolUserInputQuestionPlaceholder(q)},
	}
	if value := toolUserInputDraftValue(drafts, q.ID); value != "" {
		input["default_value"] = value
	}
	return input
}

func buildToolUserInputSingleSelectElement(q toolUserInputQuestion, drafts toolUserInputFormDrafts) map[string]any {
	options := make([]selectStaticOption, 0, len(q.Options))
	for _, opt := range q.Options {
		options = append(options, selectStaticOption{
			Text:  toolUserInputOptionText(opt),
			Value: strings.TrimSpace(opt.Label),
		})
	}
	initialOption := toolUserInputInitialOption(q, toolUserInputDraftValue(drafts, q.ID))
	return buildFormSelectStaticElement(q.ID, toolUserInputQuestionPlaceholder(q), options, initialOption)
}

func buildToolUserInputOtherInputElement(q toolUserInputQuestion, drafts toolUserInputFormDrafts) map[string]any {
	input := map[string]any{
		"tag":         "input",
		"name":        toolUserInputOtherFieldName(q),
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "其它值（可选）"},
	}
	if value := toolUserInputDraftValue(drafts, toolUserInputOtherFieldName(q)); value != "" {
		input["default_value"] = value
	}
	return input
}

func buildToolUserInputMultiSelectRows(q toolUserInputQuestion, drafts toolUserInputFormDrafts, requestID string) []map[string]any {
	selected := toolUserInputMultiDraftValues(drafts, q.ID)
	selectedSet := map[string]struct{}{}
	for _, value := range selected {
		selectedSet[strings.TrimSpace(value)] = struct{}{}
	}
	buttons := make([]feishu.Button, 0, len(q.Options))
	for _, opt := range q.Options {
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			continue
		}
		text := "[ ] " + label
		buttonType := "default"
		if _, ok := selectedSet[label]; ok {
			text = "[x] " + label
			buttonType = "primary"
		}
		buttons = append(buttons, feishu.Button{
			Text: text,
			Type: buttonType,
			Name: "toggle_" + strings.TrimSpace(q.ID) + "_" + strings.ToLower(strings.ReplaceAll(label, " ", "_")),
			Value: map[string]any{
				"action":       "user_input.toggle_multi",
				"request_id":   strings.TrimSpace(requestID),
				"question_id":  q.ID,
				"option_label": label,
				"multi_drafts": toolUserInputMultiDraftActionValue(drafts.Multi),
			},
		})
	}
	rows := make([]map[string]any, 0, (len(buttons)+2)/3)
	for start := 0; start < len(buttons); start += 3 {
		end := start + 3
		if end > len(buttons) {
			end = len(buttons)
		}
		rows = append(rows, buildMarkdownBodyCardActionElement(buttons[start:end]))
	}
	return rows
}

func toolUserInputOptionText(opt toolUserInputOption) string {
	label := strings.TrimSpace(opt.Label)
	desc := strings.TrimSpace(opt.Description)
	if label == "" || desc == "" {
		return firstNonEmpty(label, desc)
	}
	return label + " - " + desc
}

func toolUserInputInitialOption(q toolUserInputQuestion, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, opt := range q.Options {
		if strings.EqualFold(strings.TrimSpace(opt.Label), raw) {
			return strings.TrimSpace(opt.Label)
		}
	}
	return ""
}

func toolUserInputOtherFieldName(q toolUserInputQuestion) string {
	return strings.TrimSpace(q.ID) + "__other"
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
	if !q.MultiSelect && len(answers) > 1 {
		return nil, fmt.Errorf("only one answer is allowed")
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

func toolUserInputDraftsFromCardAction(payload toolUserInputPayload, action *feishu.CardAction) toolUserInputFormDrafts {
	drafts := toolUserInputFormDrafts{
		Values: map[string]string{},
		Multi:  toolUserInputMultiDraftsFromActionValue(actionValueMap(action)),
	}
	for _, q := range payload.Questions {
		if value, ok := toolUserInputSelectionValue(action.FormValue, q.ID); ok {
			drafts.Values[q.ID] = value
		}
		if q.IsOther {
			if value, ok := toolUserInputSelectionValue(action.FormValue, toolUserInputOtherFieldName(q)); ok {
				drafts.Values[toolUserInputOtherFieldName(q)] = value
			}
		}
	}
	return drafts
}

func toolUserInputSelectionsFromDrafts(payload toolUserInputPayload, drafts toolUserInputFormDrafts) map[string]string {
	selections := make(map[string]string, len(payload.Questions))
	for _, q := range payload.Questions {
		switch {
		case q.MultiSelect:
			parts := append([]string(nil), toolUserInputMultiDraftValues(drafts, q.ID)...)
			if q.IsOther {
				parts = append(parts, splitAnswerParts(toolUserInputDraftValue(drafts, toolUserInputOtherFieldName(q)))...)
			}
			selections[q.ID] = strings.Join(parts, ", ")
		case len(q.Options) > 0:
			parts := splitAnswerParts(toolUserInputDraftValue(drafts, q.ID))
			if q.IsOther {
				parts = append(parts, splitAnswerParts(toolUserInputDraftValue(drafts, toolUserInputOtherFieldName(q)))...)
			}
			selections[q.ID] = strings.Join(parts, ", ")
		default:
			selections[q.ID] = toolUserInputDraftValue(drafts, q.ID)
		}
	}
	return selections
}

func toolUserInputDraftValue(drafts toolUserInputFormDrafts, key string) string {
	if drafts.Values == nil {
		return ""
	}
	return strings.TrimSpace(drafts.Values[strings.TrimSpace(key)])
}

func toolUserInputMultiDraftValues(drafts toolUserInputFormDrafts, key string) []string {
	if drafts.Multi == nil {
		return nil
	}
	values := drafts.Multi[strings.TrimSpace(key)]
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func toolUserInputMultiDraftActionValue(drafts map[string][]string) map[string]any {
	if len(drafts) == 0 {
		return map[string]any{}
	}
	keys := make([]string, 0, len(drafts))
	for key := range drafts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		values := drafts[key]
		if len(values) == 0 {
			continue
		}
		copied := make([]any, 0, len(values))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" {
				copied = append(copied, value)
			}
		}
		if len(copied) > 0 {
			out[key] = copied
		}
	}
	return out
}

func toolUserInputMultiDraftsFromActionValue(values map[string]any) map[string][]string {
	raw, ok := values["multi_drafts"]
	if !ok {
		return map[string][]string{}
	}
	result := map[string][]string{}
	switch typed := raw.(type) {
	case map[string]any:
		for key, value := range typed {
			if values := toolUserInputMultiDraftList(value); len(values) > 0 {
				result[strings.TrimSpace(key)] = values
			}
		}
	case map[string]string:
		for key, value := range typed {
			if values := splitAnswerParts(value); len(values) > 0 {
				result[strings.TrimSpace(key)] = values
			}
		}
	}
	return result
}

func toolUserInputMultiDraftList(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return uniqueToolUserInputParts(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := normalizeToolUserInputSelection(item); ok && strings.TrimSpace(value) != "" {
				parts = append(parts, strings.TrimSpace(value))
			}
		}
		return uniqueToolUserInputParts(parts)
	case string:
		return uniqueToolUserInputParts(splitAnswerParts(typed))
	}
	return nil
}

func uniqueToolUserInputParts(parts []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(part)]; ok {
			continue
		}
		seen[strings.ToLower(part)] = struct{}{}
		out = append(out, part)
	}
	return out
}

func toggleToolUserInputMultiDraft(drafts toolUserInputFormDrafts, questionID, optionLabel string) toolUserInputFormDrafts {
	if drafts.Multi == nil {
		drafts.Multi = map[string][]string{}
	}
	questionID = strings.TrimSpace(questionID)
	optionLabel = strings.TrimSpace(optionLabel)
	current := toolUserInputMultiDraftValues(drafts, questionID)
	next := make([]string, 0, len(current))
	found := false
	for _, value := range current {
		if strings.EqualFold(value, optionLabel) {
			found = true
			continue
		}
		next = append(next, value)
	}
	if !found && optionLabel != "" {
		next = append(next, optionLabel)
	}
	if len(next) == 0 {
		delete(drafts.Multi, questionID)
		return drafts
	}
	drafts.Multi[questionID] = next
	return drafts
}

func actionValueMap(action *feishu.CardAction) map[string]any {
	if action == nil || action.ActionValue == nil {
		return map[string]any{}
	}
	return action.ActionValue
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
