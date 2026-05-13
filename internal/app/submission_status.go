package app

import (
	"strings"

	"feidex/internal/app/apputil"
	appturnlifecycle "feidex/internal/app/turnlifecycle"
	"feidex/internal/config"
	"feidex/internal/state"
)

var turnCompletionTerminalText = appturnlifecycle.TurnCompletionTerminalText

func prepareSubmissionCardMarkdown(a *App, sub *state.Submission, text string) string {
	text = strings.TrimSpace(text)
	if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
		text = sanitizeLocalMarkdownLinks(text, ws.Cwd)
	}
	return normalizeCardMarkdown(text)
}

var truncate = apputil.Truncate
