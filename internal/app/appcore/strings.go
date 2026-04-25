package appcore

import (
	"encoding/json"
	"strings"
	"unicode"
)

// FirstNonEmpty returns the first non-empty string from the given values.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// MustJSON marshals v to JSON, returning "{}" on error.
func MustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// StringPtrValue dereferences a string pointer, returning "" if nil.
func StringPtrValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Truncate truncates a string to the given limit, appending "..." if needed.
func Truncate(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "..."
}

// LabelledValue formats a labelled value for markdown display.
func LabelledValue(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return label + ": " + value
}

// JoinNonEmpty joins non-empty strings with the given separator.
func JoinNonEmpty(parts []string, sep string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			nonEmpty = append(nonEmpty, strings.TrimSpace(p))
		}
	}
	return strings.Join(nonEmpty, sep)
}

// EscapeInlineBackticks escapes backticks in a string for markdown inline code.
func EscapeInlineBackticks(s string) string {
	return strings.ReplaceAll(s, "`", "` + \"`\" + `")
}

// UniqueStrings returns a new slice with duplicates removed, preserving order.
func UniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

// RemoveString removes the first occurrence of target from the slice.
func RemoveString(items []string, target string) []string {
	for i, item := range items {
		if item == target {
			return append(items[:i], items[i+1:]...)
		}
	}
	return items
}

// NormalizeCardMarkdown normalizes markdown content for card display.
func NormalizeCardMarkdown(s string) string {
	return strings.TrimSpace(s)
}

// ParseBacktickFenceLine parses a backtick fence line, returning the fence
// length and info string.
func ParseBacktickFenceLine(line string) (int, string) {
	line = strings.TrimSpace(line)
	if len(line) < 3 {
		return 0, ""
	}
	backticks := 0
	for _, r := range line {
		if r == '`' {
			backticks++
		} else {
			break
		}
	}
	if backticks < 3 {
		return 0, ""
	}
	info := strings.TrimSpace(line[backticks:])
	return backticks, info
}

// CountLeadingBackticks counts leading backticks in a string.
func CountLeadingBackticks(s string) int {
	count := 0
	for _, r := range s {
		if r == '`' {
			count++
		} else {
			break
		}
	}
	return count
}

// AttentionMentionMarkdown creates a markdown mention for the given user ID.
func AttentionMentionMarkdown(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	return "<at user_id=\"" + userID + "\"></at>"
}

// PrependAttentionMentionMarkdown prepends an attention mention to markdown.
func PrependAttentionMentionMarkdown(userID, body string) string {
	mention := AttentionMentionMarkdown(userID)
	if mention == "" {
		return body
	}
	return mention + "\n" + body
}

// IsAlphaNum returns true if the byte is alphanumeric.
func IsAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// IsFileNameLike returns true if the string looks like a filename.
func IsFileNameLike(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 256 {
		return false
	}
	hasDot := false
	for _, r := range s {
		switch {
		case r == '.':
			hasDot = true
		case r == '/' || r == '\\':
			return false
		case unicode.IsSpace(r):
			return false
		}
	}
	return hasDot
}
