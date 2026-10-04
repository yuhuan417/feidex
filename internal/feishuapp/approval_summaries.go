package feishuapp

import (
	"feidex/internal/adapter/feishu/planmode"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/feishu"
	apputil "feidex/internal/formatutil"
	"strings"
)

func renderApprovalCard(state planmode.SessionStateProvider, client FeishuClient, sub *domainsubmission.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	attentionUserID := ""
	if sub != nil {
		attentionUserID = sub.UserID
	}
	title = contentCardTitleForSubmission(state, sub, title)
	return client.SimpleStatusCard(title, color, apputil.PrependAttentionMentionMarkdown(strings.TrimSpace(body), attentionUserID), buttons)
}
