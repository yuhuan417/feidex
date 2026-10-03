package app

import (
	"feidex/internal/adapter/feishu/attachments"
	domainsubmission "feidex/internal/domain/submission"
	"strings"

	applinkutil "feidex/internal/adapter/feishu/linkutil"
	"feidex/internal/config"
)

func prepareSubmissionCardMarkdown(a *App, sub *domainsubmission.Submission, text string) string {
	text = strings.TrimSpace(text)
	text = applinkutil.LinkifyInlineCodeURLs(text)
	if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
		text = attachments.NeutralizeLocalMarkdownLinks(text, ws.Cwd)
	}
	return applinkutil.NormalizeCardMarkdown(text)
}
