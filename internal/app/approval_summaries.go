package app

import (
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func renderApprovalCard(a *App, _ string, sub *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	attentionUserID := ""
	if sub != nil {
		attentionUserID = sub.UserID
	}
	return a.feishu.SimpleStatusCard(title, color, prependAttentionMentionMarkdown(strings.TrimSpace(body), attentionUserID), buttons)
}

func firstNonEmptyValue(values ...any) any {
	for _, value := range values {
		switch x := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(x) != "" {
				return value
			}
		case []any:
			if len(x) > 0 {
				return value
			}
		case []string:
			if len(x) > 0 {
				return value
			}
		case map[string]any:
			if len(x) > 0 {
				return value
			}
		default:
			return value
		}
	}
	return nil
}
