package app

import (
	"strings"

	"feidex/internal/config"
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

func turnCompletionTerminalText(status, lastError string) string {
	lastError = strings.TrimSpace(lastError)
	if status == "completed" {
		return ""
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
		return "任务已中断。"
	default:
		return fallback
	}
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

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
