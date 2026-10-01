package app

import (
	"strings"

	"feidex/internal/app/attachments"
	applinkutil "feidex/internal/app/linkutil"
	"feidex/internal/config"
	"feidex/internal/state"
)

func prepareSubmissionCardMarkdown(a *App, sub *state.Submission, text string) string {
	text = strings.TrimSpace(text)
	text = applinkutil.LinkifyInlineCodeURLs(text)
	if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
		text = attachments.NeutralizeLocalMarkdownLinks(text, ws.Cwd)
	}
	return applinkutil.NormalizeCardMarkdown(text)
}
