package app

import (
	"strings"

	"feidex/internal/app/apputil"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func renderApprovalCard(a *App, _ string, sub *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	attentionUserID := ""
	if sub != nil {
		attentionUserID = sub.UserID
	}
	title = contentCardTitleForSubmission(a, sub, title)
	return a.feishu.SimpleStatusCard(title, color, apputil.PrependAttentionMentionMarkdown(strings.TrimSpace(body), attentionUserID), buttons)
}
