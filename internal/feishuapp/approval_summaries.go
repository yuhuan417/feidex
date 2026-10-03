package feishuapp

import (
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
	apputil "feidex/internal/formatutil"
	"strings"
)

func renderApprovalCard(a *App, _ string, sub *domainsubmission.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	attentionUserID := ""
	if sub != nil {
		attentionUserID = sub.UserID
	}
	title = contentCardTitleForSubmission(a, sub, title)
	return a.feishu.SimpleStatusCard(title, color, apputil.PrependAttentionMentionMarkdown(strings.TrimSpace(body), attentionUserID), buttons)
}
