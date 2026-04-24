package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (a *App) sendElicitationFormCard(requestID json.RawMessage, payload elicitationFormPayload) {
	sessionKey, sub := a.findSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil {
		a.replyCodexError(requestID, -32602, "no active session for elicitation")
		return
	}
	requestKey := requestIDKey(requestID)
	card := a.feishu.SimpleStatusCard("需要补充表单", "orange", prependAttentionMentionMarkdown(renderElicitationFormBody(payload), sub.UserID), []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestKey}},
	})
	err := a.deliverPendingCard(sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: requestIDStored(requestID),
		backend:         backendCodex,
		kind:            "mcp_elicitation_form",
		sessionKey:      sessionKey,
		threadID:        payload.ThreadID,
		turnID:          payload.TurnID,
		ownerUserID:     sub.UserID,
		payloadJSON:     mustJSON(payload),
		waitingStatus:   "waiting_user_input",
		linkKind:        "elicitation_form_card",
	})
	if err == nil {
		return
	}
	a.replyCodexError(requestID, -32603, err.Error())
}

func (a *App) sendElicitationURLCard(requestID json.RawMessage, payload elicitationURLPayload) {
	sessionKey, sub := a.findSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil {
		a.replyCodexError(requestID, -32602, "no active session for elicitation")
		return
	}
	requestKey := requestIDKey(requestID)
	body := payload.Message
	if strings.TrimSpace(payload.URL) != "" {
		body += "\n\n打开链接：<" + payload.URL + ">"
	}
	card := a.feishu.SimpleStatusCard("外部表单", "orange", prependAttentionMentionMarkdown(body, sub.UserID), []feishu.Button{
		{Text: "已完成", Type: "primary", Value: map[string]any{"action": "elicitation_url.accept", "request_id": requestKey}},
		{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "elicitation_url.decline", "request_id": requestKey}},
		{Text: "取消", Type: "default", Value: map[string]any{"action": "elicitation_url.cancel", "request_id": requestKey}},
	})
	err := a.deliverPendingCard(sub, card, pendingCardDelivery{
		requestKey:      requestKey,
		requestIDStored: requestIDStored(requestID),
		backend:         backendCodex,
		kind:            "mcp_elicitation_url",
		sessionKey:      sessionKey,
		threadID:        payload.ThreadID,
		turnID:          payload.TurnID,
		ownerUserID:     sub.UserID,
		payloadJSON:     mustJSON(payload),
		waitingStatus:   "waiting_user_input",
		linkKind:        "elicitation_url_card",
	})
	if err == nil {
		return
	}
	a.replyCodexError(requestID, -32603, err.Error())
}

func (a *App) completeElicitationURLAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != "mcp_elicitation_url" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	adapter := a.serverRequestBackendAdapter(pending)
	decision, err := adapter.replyElicitationURL(pending, actionName)
	if err != nil {
		slog.Error("elicitation url reply failed",
			"backend", adapter.kind(),
			"request_id", requestID,
			"action", actionName,
			"user_id", action.UserID,
			"error", err,
		)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "提交失败，请重试"},
		}, nil
	}
	_ = newRuntimeStateService(a).finalizePendingReply(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card:  rawCard(a.feishu.SimpleStatusCard("已处理", "green", "已提交 "+decision+"。", nil)),
	}, nil
}

func (s pendingInputService) completeElicitationFormText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	var payload elicitationFormPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return err
	}
	adapter := s.app.serverRequestBackendAdapter(pending)
	summary, err := adapter.replyElicitationForm(pending, payload, msg.Text)
	if err != nil {
		return err
	}
	_ = newRuntimeStateService(s.app).finalizePendingReply(pending)
	if pending.FeishuMsgID != "" {
		_ = s.app.feishu.PatchCard(context.Background(), pending.FeishuMsgID, s.app.feishu.SimpleStatusCard("已提交", "green", summary, nil))
	}
	return nil
}

func renderElicitationFormBody(payload elicitationFormPayload) string {
	lines := []string{payload.Message, "", "请直接回复下一条消息提交表单。"}
	if properties, _ := payload.Schema["properties"].(map[string]any); len(properties) > 0 {
		keys := sortedMapKeys(properties)
		required := requiredSet(payload.Schema)
		for _, key := range keys {
			field, _ := properties[key].(map[string]any)
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("%s%s", displayFieldTitle(key, field), requiredMarker(required[key])))
			if desc := stringField(field, "description"); desc != "" {
				lines = append(lines, desc)
			}
			if options := schemaOptionLabels(field); len(options) > 0 {
				lines = append(lines, "可选值: "+strings.Join(options, ", "))
			}
		}
	}
	lines = append(lines, "", "多字段请按以下格式：", "field_name: value")
	return strings.Join(lines, "\n")
}

func parseElicitationFormResponse(text string, payload elicitationFormPayload) (map[string]any, string, error) {
	properties, _ := payload.Schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return nil, "", fmt.Errorf("empty elicitation schema")
	}
	answerMap := parseStructuredLines(text)
	required := requiredSet(payload.Schema)
	content := map[string]any{}
	summaryLines := make([]string, 0, len(properties))
	for _, key := range sortedMapKeys(properties) {
		field, _ := properties[key].(map[string]any)
		raw := strings.TrimSpace(answerMap[key])
		if raw == "" && len(properties) == 1 && len(answerMap) == 0 {
			raw = text
		}
		if raw == "" {
			if required[key] {
				return nil, "", fmt.Errorf("%s: answer is required", key)
			}
			continue
		}
		value, summary, err := parseElicitationFieldValue(raw, field)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", key, err)
		}
		content[key] = value
		summaryLines = append(summaryLines, fmt.Sprintf("`%s`: %s", key, summary))
	}
	return content, strings.Join(summaryLines, "\n"), nil
}

func parseElicitationFieldValue(raw string, field map[string]any) (any, string, error) {
	raw = strings.TrimSpace(raw)
	switch fieldType(field) {
	case "boolean":
		v, err := parseBool(raw)
		if err != nil {
			return nil, "", err
		}
		return v, strconv.FormatBool(v), nil
	case "number":
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, "", fmt.Errorf("expect number")
		}
		return v, raw, nil
	case "integer":
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("expect integer")
		}
		return v, raw, nil
	case "array":
		parts := splitAnswerParts(raw)
		if len(parts) == 0 {
			return nil, "", fmt.Errorf("expect one or more values")
		}
		return parts, strings.Join(parts, ", "), nil
	default:
		if options := schemaOptionLabels(field); len(options) > 0 {
			matched, err := matchSchemaOption(raw, field)
			if err != nil {
				return nil, "", err
			}
			return matched, matched, nil
		}
		return raw, raw, nil
	}
}

func requiredSet(schema map[string]any) map[string]bool {
	required := map[string]bool{}
	values, _ := schema["required"].([]any)
	for _, v := range values {
		if s, ok := v.(string); ok {
			required[s] = true
		}
	}
	return required
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func displayFieldTitle(key string, field map[string]any) string {
	if title := stringField(field, "title"); title != "" {
		return title
	}
	return key
}

func requiredMarker(required bool) string {
	if required {
		return " *"
	}
	return ""
}

func stringField(field map[string]any, key string) string {
	if field == nil {
		return ""
	}
	if v, ok := field[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func fieldType(field map[string]any) string {
	if field == nil {
		return ""
	}
	if v, ok := field["type"].(string); ok {
		return v
	}
	return ""
}

func schemaOptionLabels(field map[string]any) []string {
	switch {
	case field == nil:
		return nil
	case field["enum"] != nil:
		values, _ := field["enum"].([]any)
		out := make([]string, 0, len(values))
		names, _ := field["enumNames"].([]any)
		for i, v := range values {
			value, _ := v.(string)
			label := value
			if i < len(names) {
				if s, ok := names[i].(string); ok && strings.TrimSpace(s) != "" {
					label = s
				}
			}
			out = append(out, label)
		}
		return out
	case field["oneOf"] != nil:
		values, _ := field["oneOf"].([]any)
		out := make([]string, 0, len(values))
		for _, v := range values {
			item, _ := v.(map[string]any)
			if title := stringField(item, "title"); title != "" {
				out = append(out, title)
				continue
			}
			if c := stringField(item, "const"); c != "" {
				out = append(out, c)
			}
		}
		return out
	case field["items"] != nil:
		items, _ := field["items"].(map[string]any)
		if anyOf, _ := items["anyOf"].([]any); len(anyOf) > 0 {
			out := make([]string, 0, len(anyOf))
			for _, v := range anyOf {
				item, _ := v.(map[string]any)
				if title := stringField(item, "title"); title != "" {
					out = append(out, title)
					continue
				}
				if c := stringField(item, "const"); c != "" {
					out = append(out, c)
				}
			}
			return out
		}
	}
	return nil
}

func matchSchemaOption(raw string, field map[string]any) (string, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case field["enum"] != nil:
		values, _ := field["enum"].([]any)
		names, _ := field["enumNames"].([]any)
		for i, v := range values {
			value, _ := v.(string)
			if strings.EqualFold(raw, value) {
				return value, nil
			}
			if i < len(names) {
				if label, ok := names[i].(string); ok && strings.EqualFold(raw, label) {
					return value, nil
				}
			}
		}
	case field["oneOf"] != nil:
		values, _ := field["oneOf"].([]any)
		for _, v := range values {
			item, _ := v.(map[string]any)
			value := stringField(item, "const")
			title := stringField(item, "title")
			if strings.EqualFold(raw, value) || (title != "" && strings.EqualFold(raw, title)) {
				return value, nil
			}
		}
	case field["items"] != nil:
		return raw, nil
	}
	return "", fmt.Errorf("unsupported option %q", raw)
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "yes", "y", "1", "是":
		return true, nil
	case "false", "no", "n", "0", "否":
		return false, nil
	default:
		return false, fmt.Errorf("expect boolean yes/no")
	}
}
