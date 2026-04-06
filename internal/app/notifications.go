package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) handleNotification(method string, params json.RawMessage) {
	slog.Info("codex notification", "method", method)
	switch method {
	case "item/agentMessage/delta":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Delta    string `json:"delta"`
		}
		if json.Unmarshal(params, &p) == nil {
			a.updateSubmissionByTurn(p.ThreadID, p.TurnID, func(sub *state.Submission) {
				sub.OutputText += p.Delta
			})
		}
	case "item/reasoning/summaryTextDelta":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Delta    string `json:"delta"`
		}
		if json.Unmarshal(params, &p) == nil {
			a.updateSubmissionByTurn(p.ThreadID, p.TurnID, func(sub *state.Submission) {
				sub.SummaryText += p.Delta
			})
		}
	case "item/commandExecution/outputDelta":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Delta    string `json:"delta"`
		}
		if json.Unmarshal(params, &p) == nil {
			a.updateSubmissionByTurn(p.ThreadID, p.TurnID, func(sub *state.Submission) {
				sub.CommandText += p.Delta
				sub.CommandText = lastN(sub.CommandText, 1200)
			})
		}
	case "turn/plan/updated":
		var p struct {
			TurnID string `json:"turnId"`
			Plan   []struct {
				Step   string `json:"step"`
				Status string `json:"status"`
			} `json:"plan"`
		}
		if json.Unmarshal(params, &p) == nil {
			plan := make([]string, 0, len(p.Plan))
			for _, item := range p.Plan {
				plan = append(plan, fmt.Sprintf("- [%s] %s", item.Status, item.Step))
			}
			a.updateSubmissionByTurn("", p.TurnID, func(sub *state.Submission) {
				sub.PlanText = strings.Join(plan, "\n")
			})
		}
	case "turn/completed":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) == nil {
			slog.Info("turn completed",
				"thread_id", p.ThreadID,
				"turn_id", p.Turn.ID,
				"status", p.Turn.Status,
			)
			a.finishTurn(p.ThreadID, p.Turn.ID, p.Turn.Status)
		}
	case "error":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Error    struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(params, &p) == nil {
			slog.Error("codex turn error",
				"thread_id", p.ThreadID,
				"turn_id", p.TurnID,
				"message", p.Error.Message,
			)
			a.updateSubmissionByTurn(p.ThreadID, p.TurnID, func(sub *state.Submission) {
				sub.Status = "failed"
				sub.SummaryText = p.Error.Message
			})
		}
	case "serverRequest/resolved":
		// no-op for MVP
	}
}

func (a *App) handleServerRequest(req codexrpc.RequestEnvelope) {
	slog.Info("codex server request", "method", req.Method)
	switch req.Method {
	case "item/commandExecution/requestApproval":
		a.onCommandApproval(req)
	case "item/fileChange/requestApproval":
		a.onFileApproval(req)
	case "item/tool/requestUserInput":
		a.onToolUserInput(req)
	default:
		_ = a.codex.ReplyError(req.ID, -32601, "unsupported server request")
	}
}

func (a *App) onCommandApproval(req codexrpc.RequestEnvelope) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
		Command  string `json:"command"`
		Cwd      string `json:"cwd"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	a.sendApprovalCard("command", req.ID, p.ThreadID, p.TurnID, p.ItemID, fmt.Sprintf("命令审批\n`%s`\n%s", p.Command, p.Reason))
}

func (a *App) onFileApproval(req codexrpc.RequestEnvelope) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	a.sendApprovalCard("file", req.ID, p.ThreadID, p.TurnID, p.ItemID, "文件变更审批\n"+p.Reason)
}

func (a *App) onToolUserInput(req codexrpc.RequestEnvelope) {
	var p struct {
		ThreadID  string `json:"threadId"`
		TurnID    string `json:"turnId"`
		ItemID    string `json:"itemId"`
		Questions []struct {
			ID       string `json:"id"`
			Question string `json:"question"`
			Options  []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	if len(p.Questions) == 1 && len(p.Questions[0].Options) > 0 && len(p.Questions[0].Options) <= 3 {
		a.sendUserInputCard(req.ID, p)
		return
	}
	_ = a.codex.ReplyError(req.ID, -32601, "complex request_user_input is not yet supported in this build")
}

func (a *App) updateSubmissionByTurn(threadID, turnID string, mutate func(*state.Submission)) {
	_, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	_ = a.store.UpdateSubmission(sub.ID, mutate)
	_ = a.refreshStatusCard(sub.ID)
}

func (a *App) finishTurn(threadID, turnID, status string) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		slog.Warn("finishTurn missing submission",
			"thread_id", threadID,
			"turn_id", turnID,
			"status", status,
		)
		return
	}
	if sub.Finalized {
		slog.Info("finishTurn ignored finalized submission",
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}
	switch status {
	case "completed":
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
			s.Status = "completed"
			s.Finalized = true
		})
	case "interrupted":
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
			s.Status = "interrupted"
			s.Finalized = true
		})
	default:
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
			s.Status = "failed"
			s.Finalized = true
		})
	}
	sub = a.store.GetSubmission(sub.ID)
	if sub != nil {
		slog.Info("submission finalized",
			"submission_id", sub.ID,
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
			"status", sub.Status,
		)
		finalText := strings.TrimSpace(sub.OutputText)
		if finalText == "" {
			finalText = "任务已结束。"
		}
		inThread := false
		sess := a.store.GetSession(sessionKey)
		if sess != nil && sess.ChatType == "group" {
			inThread = a.cfg.Feishu.ReplyInThread
		}
		_ = a.feishu.ReplyText(context.Background(), sub.TriggerMessageID, finalText, inThread)
		_ = a.refreshStatusCard(sub.ID)
	}
	sess := a.store.GetSession(sessionKey)
	if sess != nil {
		sess.ActiveTurnID = ""
		sess.ActiveSubmissionID = ""
		sess.Status = "idle"
		_ = a.store.UpsertSession(sess)
		slog.Info("session cleared active turn",
			"session_key", sessionKey,
			"thread_id", sess.ActiveThreadID,
		)
		_ = a.startNextSubmission(sessionKey)
	}
}

func (a *App) findSubmissionByTurn(threadID, turnID string) (string, *state.Submission) {
	// MVP: linear scan is acceptable.
	for _, sess := range a.store.AllSessions() {
		if turnID != "" && sess.ActiveTurnID == turnID {
			sub := a.store.GetSubmission(sess.ActiveSubmissionID)
			if sub != nil {
				return sess.Key, sub
			}
		}
	}
	return "", nil
}

func (a *App) sendStatusCardForSubmission(sub *state.Submission, msg *feishu.InboundMessage, status string) error {
	card := a.renderSubmissionCard(sub, status)
	id, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	if err == nil && id != "" {
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.StatusCardID = id })
	}
	return err
}

func (a *App) refreshStatusCard(submissionID string) error {
	sub := a.store.GetSubmission(submissionID)
	if sub == nil || sub.StatusCardID == "" {
		return nil
	}
	return a.feishu.PatchCard(context.Background(), sub.StatusCardID, a.renderSubmissionCard(sub, sub.Status))
}

func (a *App) renderSubmissionCard(sub *state.Submission, status string) map[string]any {
	title := "排队中"
	color := "grey"
	switch status {
	case "running", "turn_in_progress":
		title = "运行中"
		color = "blue"
	case "waiting_approval":
		title = "等待审批"
		color = "orange"
	case "waiting_user_input":
		title = "等待输入"
		color = "orange"
	case "completed":
		title = "已完成"
		color = "green"
	case "failed":
		title = "失败"
		color = "red"
	case "interrupted":
		title = "已中断"
		color = "grey"
	}
	body := fmt.Sprintf(
		"输入: %s\n\n摘要: %s\n\n调试:\n- submission: `%s`\n- session: `%s`\n- thread: `%s`\n- turn: `%s`\n- finalized: `%t`",
		truncate(sub.InputText, 120),
		truncate(nonEmpty(sub.SummaryText, sub.CommandText, sub.PlanText, sub.OutputText), 400),
		sub.ID,
		sub.SessionKey,
		firstNonEmpty(sub.ThreadID, "-"),
		firstNonEmpty(sub.TurnID, "-"),
		sub.Finalized,
	)
	buttons := []feishu.Button{
		{Text: "中断", Type: "danger", Value: map[string]any{"action": "menu.interrupt", "session_key": sub.SessionKey}},
		{Text: "线程列表", Type: "default", Value: map[string]any{"action": "menu.threads", "session_key": sub.SessionKey}},
	}
	return a.feishu.SimpleStatusCard(title, color, body, buttons)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func nonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func (a *App) sendApprovalCard(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for approval")
		return
	}
	requestKey := string(requestID)
	card := a.feishu.SimpleStatusCard("等待审批", "orange", body, []feishu.Button{
		{Text: "允许", Type: "primary", Value: map[string]any{"action": "approval." + kind + ".accept", "request_id": requestKey}},
		{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval." + kind + ".decline", "request_id": requestKey}},
	})
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		_ = a.store.UpsertPending(&state.PendingRequest{
			ID:          requestKey,
			Kind:        kind,
			SessionKey:  sessionKey,
			ThreadID:    threadID,
			TurnID:      turnID,
			ItemID:      itemID,
			OwnerUserID: sub.UserID,
			FeishuMsgID: msgID,
			Status:      "pending",
			CreatedAt:   time.Now().Unix(),
			ExpiresAt:   time.Now().Add(30 * time.Minute).Unix(),
		})
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "waiting_approval" })
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func (a *App) sendUserInputCard(requestID json.RawMessage, payload struct {
	ThreadID  string `json:"threadId"`
	TurnID    string `json:"turnId"`
	ItemID    string `json:"itemId"`
	Questions []struct {
		ID       string `json:"id"`
		Question string `json:"question"`
		Options  []struct {
			Label string `json:"label"`
		} `json:"options"`
	} `json:"questions"`
}) {
	sessionKey, sub := a.findSubmissionByTurn(payload.ThreadID, payload.TurnID)
	if sub == nil || len(payload.Questions) == 0 {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for request_user_input")
		return
	}
	q := payload.Questions[0]
	buttons := make([]feishu.Button, 0, len(q.Options))
	for _, opt := range q.Options {
		buttons = append(buttons, feishu.Button{
			Text: opt.Label,
			Type: "default",
			Value: map[string]any{
				"action":      "user_input.answer",
				"request_id":  string(requestID),
				"question_id": q.ID,
				"answer":      opt.Label,
			},
		})
	}
	card := a.feishu.SimpleStatusCard("需要补充输入", "orange", q.Question, buttons)
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		_ = a.store.UpsertPending(&state.PendingRequest{
			ID:          string(requestID),
			Kind:        "tool_request_user_input",
			SessionKey:  sessionKey,
			ThreadID:    payload.ThreadID,
			TurnID:      payload.TurnID,
			ItemID:      payload.ItemID,
			OwnerUserID: sub.UserID,
			FeishuMsgID: msgID,
			PayloadJSON: mustJSON(payload),
			Status:      "pending",
			CreatedAt:   time.Now().Unix(),
			ExpiresAt:   time.Now().Add(30 * time.Minute).Unix(),
		})
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "waiting_user_input" })
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
