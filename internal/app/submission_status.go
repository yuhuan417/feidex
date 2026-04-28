package app

import (
	"strings"

	"feidex/internal/app/apputil"
	appturnlifecycle "feidex/internal/app/turnlifecycle"
	"feidex/internal/config"
	"feidex/internal/state"
)

func updateSubmissionByTurn(a *App, threadID, turnID string, mutate func(*state.Submission)) {
	appState := a.State()
	_, sub := findSubmissionByTurn(a, threadID, turnID)
	if sub == nil {
		return
	}
	_ = appState.UpdateSubmission(sub.ID, mutate)
}

var turnCompletionTerminalText = appturnlifecycle.TurnCompletionTerminalText

func findSubmissionByTurn(a *App, threadID, turnID string) (string, *state.Submission) {
	appState := a.State()
	if strings.TrimSpace(turnID) != "" {
		if sessionKey, sub := newRuntimeStateService(a).boundSubmissionForTurn(turnID); sub != nil {
			return sessionKey, sub
		}
		for _, sess := range appState.Sessions() {
			if sess == nil {
				continue
			}
			op := sessionFindActiveOperationByTurn(sess, turnID)
			if op == nil || strings.TrimSpace(op.SubmissionID) == "" {
				continue
			}
			sub := appState.Submission(op.SubmissionID)
			if sub != nil {
				return sess.Key, sub
			}
		}
		return "", nil
	}
	if strings.TrimSpace(threadID) != "" {
		for _, sess := range appState.Sessions() {
			if sess == nil {
				continue
			}
			op := sessionFindActiveOperationByThread(sess, threadID)
			if op == nil || strings.TrimSpace(op.SubmissionID) == "" {
				continue
			}
			sub := appState.Submission(op.SubmissionID)
			if sub != nil {
				return sess.Key, sub
			}
		}
	}
	return "", nil
}

func prepareSubmissionCardMarkdown(a *App, sub *state.Submission, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
		text = sanitizeLocalMarkdownLinks(text, ws.Cwd)
	}
	return normalizeCardMarkdown(text)
}

var truncate = apputil.Truncate
