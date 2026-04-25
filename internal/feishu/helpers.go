package feishu

import (
	"fmt"
	"strings"

	apputil "feidex/internal/app/apputil"
)

// CloneCapturedCard deep-clones a card map.
func CloneCapturedCard(card map[string]any) map[string]any {
	if card == nil {
		return nil
	}
	cloned, _ := CloneCapturedValue(card).(map[string]any)
	return cloned
}

// CloneCapturedValue deep-clones any value, handling feishu-specific types.
func CloneCapturedValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for key, item := range current {
			out[key] = CloneCapturedValue(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(current))
		for i, item := range current {
			out[i] = CloneCapturedCard(item)
		}
		return out
	case []any:
		out := make([]any, len(current))
		for i, item := range current {
			out[i] = CloneCapturedValue(item)
		}
		return out
	case []string:
		return append([]string(nil), current...)
	case []Button:
		return append([]Button(nil), current...)
	case []Attachment:
		return append([]Attachment(nil), current...)
	case []SharedFileRequest:
		return append([]SharedFileRequest(nil), current...)
	case map[string]string:
		out := make(map[string]string, len(current))
		for key, item := range current {
			out[key] = item
		}
		return out
	default:
		return current
	}
}

// RenderPermissionIssueBody renders a human-readable body for a permission issue.
func RenderPermissionIssueBody(issue *PermissionIssue) string {
	if issue == nil {
		return ""
	}
	lines := []string{"检测到飞书接口权限或鉴权失败。"}
	if api := strings.TrimSpace(issue.API); api != "" {
		lines = append(lines, "接口: `"+api+"`")
	}
	if issue.Code != 0 || strings.TrimSpace(issue.Message) != "" {
		msg := strings.TrimSpace(issue.Message)
		if msg == "" {
			msg = "-"
		}
		lines = append(lines, fmt.Sprintf("返回: code=`%d` msg=`%s`", issue.Code, EscapeInlineBackticks(msg)))
	}
	if cause := strings.TrimSpace(issue.Cause); cause != "" && cause != strings.TrimSpace(issue.Message) {
		lines = append(lines, "错误: `"+EscapeInlineBackticks(apputil.Truncate(cause, 300))+"`")
	}
	if logID := strings.TrimSpace(issue.LogID); logID != "" {
		lines = append(lines, "log_id: `"+EscapeInlineBackticks(logID)+"`")
	}
	if ts := strings.TrimSpace(issue.Troubleshooter); ts != "" {
		lines = append(lines, "排障链接: "+ts)
	}
	for _, violation := range issue.PermissionViolations {
		item := JoinNonEmpty(" | ",
			apputil.FirstNonEmpty(strings.TrimSpace(violation.Description), ""),
			LabelledValue("type", violation.Type),
			LabelledValue("subject", violation.Subject),
		)
		if item != "" {
			lines = append(lines, "权限信息: "+item)
		}
	}
	for _, detail := range issue.Details {
		item := JoinNonEmpty(" = ", strings.TrimSpace(detail.Key), strings.TrimSpace(detail.Value))
		if item != "" {
			lines = append(lines, "细节: `"+EscapeInlineBackticks(apputil.Truncate(item, 300))+"`")
		}
	}
	for _, violation := range issue.FieldViolations {
		item := JoinNonEmpty(" | ",
			LabelledValue("field", violation.Field),
			LabelledValue("value", violation.Value),
			apputil.FirstNonEmpty(strings.TrimSpace(violation.Description), ""),
		)
		if item != "" {
			lines = append(lines, "字段校验: "+item)
		}
	}
	for _, help := range issue.Helps {
		item := strings.TrimSpace(help.URL)
		if desc := strings.TrimSpace(help.Description); desc != "" {
			item = JoinNonEmpty(" | ", desc, item)
		}
		if item != "" {
			lines = append(lines, "帮助链接: "+item)
		}
	}
	return strings.Join(lines, "\n")
}

// LabelledValue formats a label=value pair, returning empty if value is blank.
func LabelledValue(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return label + "=" + value
}

// JoinNonEmpty joins non-empty values with a separator.
func JoinNonEmpty(sep string, values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, sep)
}

// EscapeInlineBackticks replaces backticks with single quotes for inline code safety.
func EscapeInlineBackticks(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "`", "'")
}
