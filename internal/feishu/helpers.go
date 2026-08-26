package feishu

import (
	"fmt"
	"strings"
	"unicode"

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
		lines = append(lines, "排障链接: "+MarkdownLink("排障链接", ts))
	}
	for _, url := range PermissionIssueApplicationURLs(issue) {
		lines = append(lines, "申请权限: "+MarkdownLink("申请权限", url))
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
		url := strings.TrimSpace(help.URL)
		label := strings.TrimSpace(help.Description)
		if label == "" {
			label = "帮助链接"
		}
		item := MarkdownLink(label, url)
		if item != "" {
			lines = append(lines, "帮助链接: "+item)
		}
	}
	return strings.Join(lines, "\n")
}

// PermissionIssueApplicationURLs extracts Feishu app permission application links
// embedded in API messages. Some Feishu APIs return the grant URL only inside
// msg/cause text instead of structured helps.
func PermissionIssueApplicationURLs(issue *PermissionIssue) []string {
	if issue == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	addFromText := func(text string) {
		for _, url := range ExtractHTTPURLs(text) {
			if !isPermissionApplicationURL(url) || seen[url] {
				continue
			}
			seen[url] = true
			out = append(out, url)
		}
	}
	addFromText(issue.Message)
	addFromText(issue.Cause)
	for _, detail := range issue.Details {
		addFromText(detail.Value)
	}
	for _, violation := range issue.PermissionViolations {
		addFromText(violation.Description)
	}
	for _, violation := range issue.FieldViolations {
		addFromText(violation.Description)
	}
	return out
}

// ExtractHTTPURLs returns URL-looking substrings from arbitrary text.
func ExtractHTTPURLs(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var out []string
	for len(text) > 0 {
		idx := nextHTTPURLIndex(text)
		if idx < 0 {
			break
		}
		text = text[idx:]
		end := strings.IndexFunc(text, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune("`<>[]\"'", r)
		})
		candidate := text
		if end >= 0 {
			candidate = text[:end]
			text = text[end:]
		} else {
			text = ""
		}
		candidate = strings.TrimRight(strings.TrimSpace(candidate), ".,;，。；、!！?？)")
		if candidate != "" {
			out = append(out, candidate)
		}
	}
	return out
}

func nextHTTPURLIndex(text string) int {
	https := strings.Index(text, "https://")
	http := strings.Index(text, "http://")
	switch {
	case https < 0:
		return http
	case http < 0:
		return https
	case https < http:
		return https
	default:
		return http
	}
}

func isPermissionApplicationURL(url string) bool {
	url = strings.ToLower(strings.TrimSpace(url))
	return strings.HasPrefix(url, "http") && strings.Contains(url, "/app/") && strings.Contains(url, "/auth")
}

// MarkdownLink formats a clickable link for Feishu lark_md fields.
func MarkdownLink(label, url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = url
	}
	return "[" + sanitizeMarkdownLinkLabel(label) + "](" + url + ")"
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
