package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type toolUserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type toolUserInputQuestion struct {
	Header   string                `json:"header"`
	ID       string                `json:"id"`
	Question string                `json:"question"`
	IsOther  bool                  `json:"isOther"`
	IsSecret bool                  `json:"isSecret"`
	Options  []toolUserInputOption `json:"options"`
}

type toolUserInputPayload struct {
	ThreadID  string                  `json:"threadId"`
	TurnID    string                  `json:"turnId"`
	ItemID    string                  `json:"itemId"`
	Questions []toolUserInputQuestion `json:"questions"`
}

type elicitationFormPayload struct {
	ServerName string         `json:"serverName"`
	ThreadID   string         `json:"threadId"`
	TurnID     string         `json:"turnId,omitempty"`
	Message    string         `json:"message"`
	Schema     map[string]any `json:"requestedSchema"`
}

type elicitationURLPayload struct {
	ServerName    string `json:"serverName"`
	ThreadID      string `json:"threadId"`
	TurnID        string `json:"turnId,omitempty"`
	ElicitationID string `json:"elicitationId"`
	Message       string `json:"message"`
	URL           string `json:"url"`
}

func (a *App) pendingTextRequest(sessionKey, userID string) *state.PendingRequest {
	pending := a.store.AllPendingRequests()
	sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt > pending[j].CreatedAt })
	for _, req := range pending {
		if req == nil || req.Status != "pending" || req.SessionKey != sessionKey {
			continue
		}
		if req.OwnerUserID != "" && req.OwnerUserID != userID {
			continue
		}
		switch req.Kind {
		case "turn_append", "tool_request_user_input_form", "mcp_elicitation_form", "workspace_new":
			return req
		}
	}
	return nil
}

func (a *App) shouldRedactInboundText(sessionKey, userID string) bool {
	req := a.pendingTextRequest(sessionKey, userID)
	if req == nil {
		return false
	}
	switch req.Kind {
	case "mcp_elicitation_form":
		return true
	case "tool_request_user_input_form":
		var payload toolUserInputPayload
		if err := json.Unmarshal([]byte(req.PayloadJSON), &payload); err != nil {
			return true
		}
		for _, q := range payload.Questions {
			if q.IsSecret {
				return true
			}
		}
	}
	return false
}

func (a *App) handlePendingTextResponse(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	if msg == nil || pending == nil {
		return nil
	}
	switch pending.Kind {
	case "turn_append":
		return a.completeTurnAppendText(msg, pending)
	case "tool_request_user_input_form":
		return a.completeToolUserInputText(msg, pending)
	case "mcp_elicitation_form":
		return a.completeElicitationFormText(msg, pending)
	case "workspace_new":
		return a.completeWorkspaceNewText(msg, pending)
	default:
		return nil
	}
}

func (a *App) sendUserInputFormCard(requestID json.RawMessage, payload toolUserInputPayload) {
	sessionKey, sub := a.findSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for request_user_input")
		return
	}
	requestKey := requestIDKey(requestID)
	card := a.feishu.SimpleStatusCard("需要补充输入", "orange", renderToolUserInputBody(payload), []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestKey}},
	})
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		a.recordMessageLink(msgID, "user_input_card", sub, requestKey)
		_ = a.store.UpsertPending(&state.PendingRequest{
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
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "waiting_user_input" })
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func (a *App) completePendingFormCancel(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	switch pending.Kind {
	case "turn_append":
		if pending.FeishuMsgID != "" {
			_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("已取消", "grey", "本次追加已取消。", nil))
		}
	case "tool_request_user_input_form":
		_ = a.codex.ReplyError(pendingRequestIDRaw(pending), -32800, "cancelled by user")
	case "mcp_elicitation_form":
		_ = a.codex.Reply(pendingRequestIDRaw(pending), map[string]any{"action": "cancel"})
	}
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	a.resumeSubmissionAfterRequest(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已取消"},
		Card:  rawCard(a.feishu.SimpleStatusCard("已取消", "grey", "该请求已取消。", nil)),
	}, nil
}

func (a *App) completeTurnAppendText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	if pending == nil || msg == nil {
		return nil
	}
	sess := a.store.GetSession(pending.SessionKey)
	if sess == nil || sess.ActiveThreadID == "" || sess.ActiveTurnID == "" {
		_ = a.store.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
		if pending.FeishuMsgID != "" {
			_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("已失效", "grey", "对应任务已结束，无法继续追加。", nil))
		}
		return fmt.Errorf("当前没有可补充的任务")
	}
	if strings.TrimSpace(pending.TurnID) != "" && sess.ActiveTurnID != pending.TurnID {
		_ = a.store.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
		if pending.FeishuMsgID != "" {
			_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("已失效", "grey", "这个任务已经结束或已切换到其他任务。", nil))
		}
		return fmt.Errorf("这个任务已经结束或已切换到其他任务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.codex.Call(ctx, "turn/steer", map[string]any{
		"threadId":       sess.ActiveThreadID,
		"expectedTurnId": sess.ActiveTurnID,
		"input": []map[string]any{
			{"type": "text", "text": strings.TrimSpace(msg.Text), "text_elements": []any{}},
		},
	}, nil); err != nil {
		return err
	}
	_ = a.store.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	if pending.FeishuMsgID != "" {
		_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("已追加", "green", truncate(strings.TrimSpace(msg.Text), 300), nil))
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已追加到当前任务。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}

func (a *App) resolvePendingTurnAppendRequests(sessionKey, userID string) {
	for _, req := range a.store.AllPendingRequests() {
		if req == nil || req.Kind != "turn_append" || req.Status != "pending" || req.SessionKey != sessionKey {
			continue
		}
		if userID != "" && req.OwnerUserID != "" && req.OwnerUserID != userID {
			continue
		}
		_ = a.store.UpdatePending(req.ID, func(p *state.PendingRequest) { p.Status = "resolved" })
		if req.FeishuMsgID != "" {
			_ = a.feishu.PatchCard(context.Background(), req.FeishuMsgID, a.feishu.SimpleStatusCard("已失效", "grey", "已被新的追加请求替代。", nil))
		}
	}
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
	if err := a.codex.Reply(pendingRequestIDRaw(pending), response); err != nil {
		return err
	}
	_ = a.store.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	a.resumeSubmissionAfterRequest(pending)
	if pending.FeishuMsgID != "" {
		_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("已提交", "green", summary, nil))
	}
	return nil
}

func (a *App) sendElicitationFormCard(requestID json.RawMessage, payload elicitationFormPayload) {
	sessionKey, sub := a.findSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for elicitation")
		return
	}
	requestKey := requestIDKey(requestID)
	card := a.feishu.SimpleStatusCard("需要补充表单", "orange", renderElicitationFormBody(payload), []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestKey}},
	})
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		a.recordMessageLink(msgID, "elicitation_form_card", sub, requestKey)
		_ = a.store.UpsertPending(&state.PendingRequest{
			ID:           requestKey,
			RequestIDRaw: requestIDStored(requestID),
			Kind:         "mcp_elicitation_form",
			SessionKey:   sessionKey,
			ThreadID:     payload.ThreadID,
			TurnID:       payload.TurnID,
			OwnerUserID:  sub.UserID,
			FeishuMsgID:  msgID,
			PayloadJSON:  mustJSON(payload),
			Status:       "pending",
			CreatedAt:    time.Now().Unix(),
			ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
		})
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "waiting_user_input" })
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func (a *App) sendElicitationURLCard(requestID json.RawMessage, payload elicitationURLPayload) {
	sessionKey, sub := a.findSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for elicitation")
		return
	}
	requestKey := requestIDKey(requestID)
	body := payload.Message
	if strings.TrimSpace(payload.URL) != "" {
		body += "\n\n打开链接：<" + payload.URL + ">"
	}
	card := a.feishu.SimpleStatusCard("外部表单", "orange", body, []feishu.Button{
		{Text: "已完成", Type: "primary", Value: map[string]any{"action": "elicitation_url.accept", "request_id": requestKey}},
		{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "elicitation_url.decline", "request_id": requestKey}},
		{Text: "取消", Type: "default", Value: map[string]any{"action": "elicitation_url.cancel", "request_id": requestKey}},
	})
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		a.recordMessageLink(msgID, "elicitation_url_card", sub, requestKey)
		_ = a.store.UpsertPending(&state.PendingRequest{
			ID:           requestKey,
			RequestIDRaw: requestIDStored(requestID),
			Kind:         "mcp_elicitation_url",
			SessionKey:   sessionKey,
			ThreadID:     payload.ThreadID,
			TurnID:       payload.TurnID,
			OwnerUserID:  sub.UserID,
			FeishuMsgID:  msgID,
			PayloadJSON:  mustJSON(payload),
			Status:       "pending",
			CreatedAt:    time.Now().Unix(),
			ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
		})
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "waiting_user_input" })
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func (a *App) completeElicitationURLAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := a.store.PendingByID(requestID)
	if pending == nil || pending.Kind != "mcp_elicitation_url" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"}}, nil
	}
	decision := "cancel"
	switch actionName {
	case "elicitation_url.accept":
		decision = "accept"
	case "elicitation_url.decline":
		decision = "decline"
	}
	_ = a.codex.Reply(pendingRequestIDRaw(pending), map[string]any{"action": decision})
	_ = a.store.UpdatePending(requestID, func(req *state.PendingRequest) { req.Status = "resolved" })
	a.resumeSubmissionAfterRequest(pending)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交"},
		Card:  rawCard(a.feishu.SimpleStatusCard("已处理", "green", "已提交 "+decision+"。", nil)),
	}, nil
}

func (a *App) completeElicitationFormText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	var payload elicitationFormPayload
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return err
	}
	content, summary, err := parseElicitationFormResponse(strings.TrimSpace(msg.Text), payload)
	if err != nil {
		return err
	}
	if err := a.codex.Reply(pendingRequestIDRaw(pending), map[string]any{
		"action":  "accept",
		"content": content,
	}); err != nil {
		return err
	}
	_ = a.store.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	a.resumeSubmissionAfterRequest(pending)
	if pending.FeishuMsgID != "" {
		_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, a.feishu.SimpleStatusCard("已提交", "green", summary, nil))
	}
	return nil
}

func renderToolUserInputBody(payload toolUserInputPayload) string {
	lines := []string{"请直接回复下一条消息提交答案。"}
	for _, q := range payload.Questions {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s (`%s`)", q.Question, q.ID))
		if len(q.Options) > 0 {
			opts := make([]string, 0, len(q.Options))
			for _, opt := range q.Options {
				opts = append(opts, opt.Label)
			}
			lines = append(lines, "可选值: "+strings.Join(opts, ", "))
		}
		if q.IsSecret {
			lines = append(lines, "注意: 此答案会按敏感输入处理，不写普通日志。")
		}
	}
	if len(payload.Questions) > 1 {
		lines = append(lines, "", "多题请按以下格式：", "question_id: answer", "another_id: answer1, answer2")
	}
	return strings.Join(lines, "\n")
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

func parseToolUserInputResponse(text string, payload toolUserInputPayload) (map[string]any, string, error) {
	answerMap := parseStructuredLines(text)
	result := map[string]any{"answers": map[string]any{}}
	summaryLines := make([]string, 0, len(payload.Questions))
	for _, q := range payload.Questions {
		raw := strings.TrimSpace(answerMap[q.ID])
		if raw == "" && len(payload.Questions) == 1 {
			raw = text
		}
		answers, err := parseQuestionAnswers(raw, q)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", q.ID, err)
		}
		result["answers"].(map[string]any)[q.ID] = map[string]any{"answers": answers}
		summaryLines = append(summaryLines, fmt.Sprintf("`%s`: %s", q.ID, summarizeAnswers(answers, q.IsSecret)))
	}
	return result, strings.Join(summaryLines, "\n"), nil
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

func parseStructuredLines(text string) map[string]string {
	lines := strings.Split(text, "\n")
	out := map[string]string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out
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
