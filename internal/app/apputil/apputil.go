// Package apputil provides shared utility functions used across multiple
// sub-packages of internal/app and the app package itself.
package apputil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FirstNonEmpty returns the first string from vals that is non-empty after
// trimming whitespace. If all values are empty, it returns "".
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// CopyPermissionUpdates deep-copies a slice of permission update maps.
func CopyPermissionUpdates(in []map[string]any) []map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		copied := make(map[string]any, len(item))
		for key, value := range item {
			copied[key] = value
		}
		out = append(out, copied)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FormValueString extracts a string value from a form values map.
// It returns the trimmed string value and true if the key exists,
// or "" and false otherwise.
func FormValueString(values map[string]any, key string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	raw, ok := values[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), true
	default:
		return strings.TrimSpace(fmt.Sprint(v)), true
	}
}

// StringValue performs a type assertion to string, returning "" on failure.
func StringValue(v any) string {
	s, _ := v.(string)
	return s
}

// PrettyJSON formats v as indented JSON. Returns "" on nil input or error.
func PrettyJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

// Truncate truncates s to at most n runes, appending "…" if truncated.
// If n <= 0, s is returned as-is.
func Truncate(s string, n int) string {
	runes := []rune(s)
	if n <= 0 || len(runes) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}

// MarkdownCodeBlock wraps s in a fenced code block.
// Returns "" if s is empty after trimming.
func MarkdownCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return "```\n" + s + "\n```"
}

// InlineCodeText escapes backticks in s for inline code display.
func InlineCodeText(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "`", "'")
}

// AttentionMentionMarkdown returns a Feishu at-mention markdown string for userID.
// Returns "" if userID is empty.
func AttentionMentionMarkdown(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	return "<at id=" + userID + "></at>"
}

// PrependAttentionMentionMarkdown prepends a Feishu at-mention to body for userID.
// If userID is empty, body is returned unchanged.
func PrependAttentionMentionMarkdown(body, userID string) string {
	body = strings.TrimSpace(body)
	mention := AttentionMentionMarkdown(userID)
	if mention == "" {
		return body
	}
	if body == "" {
		return mention
	}
	if strings.HasPrefix(body, mention) {
		return body
	}
	return mention + "\n\n" + body
}
