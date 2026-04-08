package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) handleNotification(method string, params json.RawMessage) {
	slog.Debug("codex notification", "method", method)
	switch method {
	case "item/agentMessage/delta":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			ItemID   string `json:"itemId"`
			Delta    string `json:"delta"`
		}
		if json.Unmarshal(params, &p) == nil {
			a.appendTurnItemDelta(p.ThreadID, p.TurnID, p.ItemID, "agent_message", p.Delta)
		}
	case "item/reasoning/summaryTextDelta":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			ItemID   string `json:"itemId"`
			Delta    string `json:"delta"`
		}
		if json.Unmarshal(params, &p) == nil {
			a.appendTurnItemDelta(p.ThreadID, p.TurnID, p.ItemID, "reasoning", p.Delta)
		}
	case "item/commandExecution/outputDelta":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			ItemID   string `json:"itemId"`
			Delta    string `json:"delta"`
		}
		if json.Unmarshal(params, &p) == nil {
			a.appendTurnItemDelta(p.ThreadID, p.TurnID, p.ItemID, "command_execution", p.Delta)
		}
	case "item/fileChange/outputDelta":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			ItemID   string `json:"itemId"`
			Delta    string `json:"delta"`
		}
		if json.Unmarshal(params, &p) == nil {
			a.appendTurnItemDelta(p.ThreadID, p.TurnID, p.ItemID, "file_change", p.Delta)
		}
	case "item/completed":
		var p struct {
			ThreadID string         `json:"threadId"`
			TurnID   string         `json:"turnId"`
			ItemID   string         `json:"itemId"`
			Item     map[string]any `json:"item"`
		}
		if json.Unmarshal(params, &p) == nil {
			if p.ItemID == "" {
				p.ItemID = strings.TrimSpace(stringValue(p.Item["id"]))
			}
			a.completeTurnItem(context.Background(), p.ThreadID, p.TurnID, p.ItemID, p.Item)
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
			a.updatePendingPlan(p.TurnID, strings.Join(plan, "\n"))
		}
	case "turn/started":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &p) == nil {
			turnID := strings.TrimSpace(firstNonEmpty(p.Turn.ID, p.TurnID))
			if turnID != "" {
				a.onTurnStartedNotification(p.ThreadID, turnID)
			}
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
			slog.Debug("turn completed",
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
			a.recordTurnError(p.ThreadID, p.TurnID, p.Error.Message)
			a.updateSubmissionByTurn(p.ThreadID, p.TurnID, func(sub *state.Submission) {
				sub.Status = "failed"
				sub.SummaryText = p.Error.Message
			})
		}
	case "serverRequest/resolved":
		var p struct {
			ThreadID  string          `json:"threadId"`
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(params, &p) == nil {
			reqID := requestIDKey(p.RequestID)
			_ = a.store.UpdatePending(reqID, func(req *state.PendingRequest) { req.Status = "resolved" })
			pending := a.store.PendingByID(reqID)
			a.resumeSubmissionAfterRequest(pending)
		}
	}
}

func (a *App) onTurnStartedNotification(threadID, turnID string) {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return
	}
	sessionKey := ""
	for _, candidate := range a.store.AllSessions() {
		if candidate == nil {
			continue
		}
		if strings.TrimSpace(candidate.ActiveThreadID) != threadID {
			continue
		}
		if strings.TrimSpace(candidate.ActiveTurnID) == turnID {
			return
		}
		if strings.TrimSpace(candidate.ActiveTurnID) != "" {
			continue
		}
		if strings.TrimSpace(candidate.ActiveSubmissionID) == "" {
			continue
		}
		sessionKey = candidate.Key
		break
	}
	if sessionKey == "" {
		return
	}
	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		slog.Warn("turn started notification missing session",
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}
	sub := a.store.GetSubmission(sess.ActiveSubmissionID)
	if sub == nil {
		slog.Warn("turn started notification missing submission",
			"session_key", sessionKey,
			"submission_id", sess.ActiveSubmissionID,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}
	sess.ActiveSubmissionID = sub.ID
	sess.ActiveTurnID = turnID
	sess.Status = "turn_in_progress"
	setSessionThreadContext(sess, sub.WorkspaceID, threadID, sess.ActiveThreadName, sess.ActiveThreadPreview)
	if err := a.store.UpsertSession(sess); err != nil {
		slog.Error("turn started notification session bind failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
		return
	}
	_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) {
		s.ThreadID = threadID
		s.TurnID = turnID
		s.Status = "running"
	})
	sub.ThreadID = threadID
	sub.TurnID = turnID
	sub.Status = "running"
	a.noteTurnStarted(sessionKey, sub)
	a.markSessionThreadLive(sessionKey, threadID)
	slog.Debug("turn started notification rebound pending submission",
		"session_key", sessionKey,
		"submission_id", sub.ID,
		"thread_id", threadID,
		"turn_id", turnID,
	)
	logSessionState("turn started notification session snapshot", sessionKey, a.store.GetSession(sessionKey))
}

func (a *App) handleServerRequest(req codexrpc.RequestEnvelope) {
	slog.Debug("codex server request", "method", req.Method)
	switch req.Method {
	case "item/commandExecution/requestApproval":
		a.onCommandApproval(req)
	case "item/fileChange/requestApproval":
		a.onFileApproval(req)
	case "item/permissions/requestApproval":
		a.onPermissionsApproval(req)
	case "item/tool/requestUserInput":
		a.onToolUserInput(req)
	case "mcpServer/elicitation/request":
		a.onMcpElicitationRequest(req)
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

func (a *App) onPermissionsApproval(req codexrpc.RequestEnvelope) {
	var p struct {
		ThreadID    string         `json:"threadId"`
		TurnID      string         `json:"turnId"`
		ItemID      string         `json:"itemId"`
		Reason      string         `json:"reason"`
		Permissions map[string]any `json:"permissions"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	body := "权限审批"
	if strings.TrimSpace(p.Reason) != "" {
		body += "\n" + p.Reason
	}
	body += "\n" + truncate(mustJSON(p.Permissions), 300)
	a.sendPermissionsCard(req.ID, p.ThreadID, p.TurnID, p.ItemID, body, p.Permissions)
}

func (a *App) onToolUserInput(req codexrpc.RequestEnvelope) {
	var p toolUserInputPayload
	if err := json.Unmarshal(req.Params, &p); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	if len(p.Questions) == 1 && len(p.Questions[0].Options) > 0 && len(p.Questions[0].Options) <= 3 {
		a.sendUserInputCard(req.ID, p)
		return
	}
	a.sendUserInputFormCard(req.ID, p)
}

func (a *App) onMcpElicitationRequest(req codexrpc.RequestEnvelope) {
	var header struct {
		ServerName string `json:"serverName"`
		ThreadID   string `json:"threadId"`
		TurnID     string `json:"turnId"`
		Mode       string `json:"mode"`
		Message    string `json:"message"`
		URL        string `json:"url"`
	}
	if err := json.Unmarshal(req.Params, &header); err != nil {
		_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
		return
	}
	switch header.Mode {
	case "url":
		var payload elicitationURLPayload
		if err := json.Unmarshal(req.Params, &payload); err != nil {
			_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
			return
		}
		a.sendElicitationURLCard(req.ID, payload)
	case "form":
		var payload elicitationFormPayload
		if err := json.Unmarshal(req.Params, &payload); err != nil {
			_ = a.codex.ReplyError(req.ID, -32602, "invalid params")
			return
		}
		a.sendElicitationFormCard(req.ID, payload)
	default:
		_ = a.codex.ReplyError(req.ID, -32601, "unsupported elicitation mode")
	}
}

func (a *App) updateSubmissionByTurn(threadID, turnID string, mutate func(*state.Submission)) {
	_, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	_ = a.store.UpdateSubmission(sub.ID, mutate)
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
		slog.Debug("finishTurn ignored finalized submission",
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
		)
		return
	}

	flush := a.flushTurnStream(context.Background(), threadID, turnID)

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
		a.clearSubmissionProcessingReactions(sub)
		slog.Debug("submission finalized",
			"submission_id", sub.ID,
			"session_key", sessionKey,
			"thread_id", threadID,
			"turn_id", turnID,
			"status", sub.Status,
		)
		replyText, terminalText := turnCompletionMessages(sub.Status, sub.OutputText, flush.LastError, flush.SentOutput)
		if replyText != "" {
			a.sendFinalMessages(context.Background(), sub, replyText, a.replyInThreadForSubmission(sub))
			flush.SentOutput = true
		}
		if terminalText != "" {
			a.sendTurnEventMessages(context.Background(), sub, terminalText, a.replyInThreadForSubmission(sub), "turn_terminal")
		}
	}
	sess := a.store.GetSession(sessionKey)
	if sess != nil {
		logSessionState("finishTurn before session clear", sessionKey, sess)
		sess.ActiveTurnID = ""
		sess.ActiveSubmissionID = ""
		sess.Status = "idle"
		_ = a.store.UpsertSession(sess)
		logSessionState("finishTurn after session clear", sessionKey, a.store.GetSession(sessionKey))
		slog.Debug("finishTurn scheduling next submission asynchronously",
			"session_key", sessionKey,
			"thread_id", sess.ActiveThreadID,
		)
		go a.startNextSubmissionAsync(sessionKey, "finishTurn")
	}
}

func (a *App) startNextSubmissionAsync(sessionKey, source string) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	if err := a.startNextSubmission(sessionKey); err != nil {
		slog.Error("async startNextSubmission failed",
			"session_key", sessionKey,
			"source", source,
			"error", err,
		)
		logSessionState("async startNextSubmission failed snapshot", sessionKey, a.store.GetSession(sessionKey))
	}
}

func turnCompletionMessages(status, outputText, lastError string, sentOutput bool) (replyText, terminalText string) {
	outputText = strings.TrimSpace(outputText)
	lastError = strings.TrimSpace(lastError)
	if !sentOutput && outputText != "" {
		replyText = outputText
	}

	fallback := lastError
	if fallback == "" {
		switch status {
		case "interrupted":
			fallback = "任务已中断。"
		case "failed":
			fallback = "任务失败。"
		default:
			fallback = "任务已结束。"
		}
	}

	switch status {
	case "interrupted":
		terminalText = "任务已中断。"
	case "completed":
		terminalText = ""
	default:
		if replyText == "" {
			terminalText = fallback
		}
	}
	return replyText, terminalText
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
	if strings.TrimSpace(threadID) != "" {
		for _, sess := range a.store.AllSessions() {
			if sess == nil {
				continue
			}
			if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
				continue
			}
			if strings.TrimSpace(sess.ActiveSubmissionID) == "" {
				continue
			}
			if strings.TrimSpace(sess.ActiveTurnID) != "" && strings.TrimSpace(turnID) != "" && sess.ActiveTurnID != turnID {
				continue
			}
			sub := a.store.GetSubmission(sess.ActiveSubmissionID)
			if sub != nil {
				return sess.Key, sub
			}
		}
	}
	return "", nil
}

func (a *App) sendStatusCardForSubmission(sub *state.Submission, msg *feishu.InboundMessage, status string) error {
	if a == nil || a.feishu == nil {
		return nil
	}
	card := a.renderSubmissionCard(sub, status)
	id, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	if err == nil && id != "" {
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.StatusCardID = id })
		a.recordMessageLink(id, "status_card", sub, "")
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
	body := a.renderSubmissionCardBody(sub)
	buttons := []feishu.Button{{Text: "线程列表", Type: "default", Value: map[string]any{"action": "menu.threads", "session_key": sub.SessionKey}}}
	switch status {
	case "queued", "running", "turn_in_progress", "waiting_approval", "waiting_user_input":
		buttons = append([]feishu.Button{{Text: "中断", Type: "danger", Value: map[string]any{"action": "menu.interrupt", "session_key": sub.SessionKey}}}, buttons...)
	}
	return a.feishu.SimpleStatusCard(title, color, body, buttons)
}

func (a *App) renderSubmissionCardBody(sub *state.Submission) string {
	parts := make([]string, 0, 2)
	if input := strings.TrimSpace(submissionInputPreview(sub)); input != "" && input != "-" {
		parts = append(parts, "输入:\n"+a.prepareSubmissionCardMarkdown(sub, input))
	}
	if content := strings.TrimSpace(a.renderSubmissionLiveContent(sub)); content != "" {
		parts = append(parts, "内容:\n"+content)
	} else {
		parts = append(parts, "内容:\n"+submissionStatusPlaceholder(sub.Status))
	}
	return strings.Join(parts, "\n\n")
}

func (a *App) renderSubmissionLiveContent(sub *state.Submission) string {
	parts := make([]string, 0, 4)
	if plan := strings.TrimSpace(sub.PlanText); plan != "" {
		parts = append(parts, "计划:\n"+a.prepareSubmissionCardMarkdown(sub, plan))
	}
	if summary := strings.TrimSpace(sub.SummaryText); summary != "" {
		parts = append(parts, "摘要:\n"+a.prepareSubmissionCardMarkdown(sub, summary))
	}
	if command := strings.TrimSpace(sub.CommandText); command != "" {
		parts = append(parts, "命令输出:\n"+a.prepareSubmissionCardMarkdown(sub, command))
	}
	if output := strings.TrimSpace(sub.OutputText); output != "" {
		parts = append(parts, "回复:\n"+a.prepareSubmissionCardMarkdown(sub, output))
	}
	return strings.Join(parts, "\n\n")
}

func (a *App) prepareSubmissionCardMarkdown(sub *state.Submission, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
		text = sanitizeLocalMarkdownLinks(text, ws.Cwd)
	}
	return normalizeCardMarkdown(text)
}

func normalizeCardMarkdown(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	fenceOpen := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			lines[i] = "```"
			fenceOpen = !fenceOpen
		}
	}
	text = strings.Join(lines, "\n")
	if fenceOpen {
		text += "\n```"
	}
	return text
}

func submissionStatusPlaceholder(status string) string {
	switch status {
	case "queued":
		return "排队中..."
	case "running", "turn_in_progress":
		return "运行中..."
	case "waiting_approval":
		return "等待审批..."
	case "waiting_user_input":
		return "等待输入..."
	case "completed":
		return "任务已结束。"
	case "interrupted":
		return "任务已中断。"
	default:
		return "任务状态未知。"
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (a *App) sendApprovalCard(kind string, requestID json.RawMessage, threadID, turnID, itemID, body string) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for approval")
		return
	}
	requestKey := requestIDKey(requestID)
	buttons := approvalButtons(kind, requestKey)
	card := a.renderApprovalCard(sessionKey, sub, "等待审批", "orange", strings.TrimSpace(body), buttons)
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		a.recordMessageLink(msgID, "approval_card", sub, requestKey)
		payload := map[string]any{}
		if kind == "command" || kind == "file" {
			payload = map[string]any{"body": body}
		}
		_ = a.store.UpsertPending(&state.PendingRequest{
			ID:           requestKey,
			RequestIDRaw: requestIDStored(requestID),
			Kind:         kind,
			SessionKey:   sessionKey,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       itemID,
			OwnerUserID:  sub.UserID,
			FeishuMsgID:  msgID,
			PayloadJSON:  mustJSON(payload),
			Status:       "pending",
			CreatedAt:    time.Now().Unix(),
			ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
		})
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "waiting_approval" })
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func approvalButtons(kind, requestKey string) []feishu.Button {
	return []feishu.Button{
		{Text: "允许一次", Type: "primary", Value: map[string]any{"action": "approval." + kind + ".accept", "request_id": requestKey}},
		{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval." + kind + ".accept_session", "request_id": requestKey}},
		{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "approval." + kind + ".decline", "request_id": requestKey}},
	}
}

func (a *App) sendPermissionsCard(requestID json.RawMessage, threadID, turnID, itemID, body string, permissions map[string]any) {
	sessionKey, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		_ = a.codex.ReplyError(requestID, -32602, "no active session for permissions approval")
		return
	}
	requestKey := requestIDKey(requestID)
	card := a.renderApprovalCard(sessionKey, sub, "权限请求", "orange", strings.TrimSpace(body), []feishu.Button{
		{Text: "本次允许", Type: "primary", Value: map[string]any{"action": "approval.permissions.accept_turn", "request_id": requestKey}},
		{Text: "本会话允许", Type: "default", Value: map[string]any{"action": "approval.permissions.accept_session", "request_id": requestKey}},
	})
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		a.recordMessageLink(msgID, "permissions_card", sub, requestKey)
		_ = a.store.UpsertPending(&state.PendingRequest{
			ID:           requestKey,
			RequestIDRaw: requestIDStored(requestID),
			Kind:         "permissions",
			SessionKey:   sessionKey,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       itemID,
			OwnerUserID:  sub.UserID,
			FeishuMsgID:  msgID,
			PayloadJSON:  mustJSON(map[string]any{"permissions": permissions, "body": body}),
			Status:       "pending",
			CreatedAt:    time.Now().Unix(),
			ExpiresAt:    time.Now().Add(30 * time.Minute).Unix(),
		})
		_ = a.store.UpdateSubmission(sub.ID, func(s *state.Submission) { s.Status = "waiting_approval" })
		_ = a.refreshStatusCard(sub.ID)
		return
	}
	_ = a.codex.ReplyError(requestID, -32603, err.Error())
}

func (a *App) renderApprovalCard(_ string, _ *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	return a.feishu.SimpleStatusCard(title, color, strings.TrimSpace(body), buttons)
}

func (a *App) sendUserInputCard(requestID json.RawMessage, payload toolUserInputPayload) {
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
				"request_id":  requestIDKey(requestID),
				"question_id": q.ID,
				"answer":      opt.Label,
			},
		})
	}
	card := a.feishu.SimpleStatusCard("需要补充输入", "orange", q.Question, buttons)
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err == nil {
		requestKey := requestIDKey(requestID)
		a.recordMessageLink(msgID, "user_input_card", sub, requestKey)
		_ = a.store.UpsertPending(&state.PendingRequest{
			ID:           requestKey,
			RequestIDRaw: requestIDStored(requestID),
			Kind:         "tool_request_user_input",
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

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
