package app

import (
	"context"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) updateSubmissionByTurn(threadID, turnID string, mutate func(*state.Submission)) {
	appState := a.appState()
	_, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return
	}
	_ = appState.updateSubmission(sub.ID, mutate)
}

func turnCompletionMessages(status, outputText, lastError string, sentOutput bool) (replyText, terminalText string) {
	outputText = strings.TrimSpace(outputText)
	lastError = strings.TrimSpace(lastError)
	if status == "completed" {
		return "", ""
	}
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
	default:
		if replyText == "" {
			terminalText = fallback
		}
	}
	return replyText, terminalText
}

func (a *App) findSubmissionByTurn(threadID, turnID string) (string, *state.Submission) {
	appState := a.appState()
	if strings.TrimSpace(turnID) != "" {
		if sessionKey, sub := a.boundSubmissionForTurn(turnID); sub != nil {
			return sessionKey, sub
		}
		for _, sess := range appState.sessions() {
			if turnID != "" && sess.ActiveTurnID == turnID {
				sub := appState.submission(sess.ActiveSubmissionID)
				if sub != nil {
					return sess.Key, sub
				}
			}
		}
		return "", nil
	}
	if strings.TrimSpace(threadID) != "" {
		for _, sess := range appState.sessions() {
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
			sub := appState.submission(sess.ActiveSubmissionID)
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
	appState := a.appState()
	card := a.renderSubmissionCard(sub, status)
	id, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	if err == nil && id != "" {
		_ = appState.setSubmissionStatusCard(sub.ID, id)
		a.recordMessageLink(id, "status_card", sub, "")
	}
	return err
}

func (a *App) refreshStatusCard(submissionID string) error {
	sub := a.appState().submission(submissionID)
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
	buttons := []feishu.Button{{Text: "线程管理", Type: "default", Value: map[string]any{"action": "menu.thread", "session_key": sub.SessionKey}}}
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
