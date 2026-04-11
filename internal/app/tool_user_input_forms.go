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
	card := a.feishu.SimpleStatusCard("需要补充输入", "orange", renderToolUserInputBody(payload), []feishu.Button{
		{Text: "取消", Type: "default", Value: map[string]any{"action": "pending_form.cancel", "request_id": requestKey}},
	})
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
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func (a *App) completeToolUserInputText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	appState := a.appState()
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
	_ = appState.updatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
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
