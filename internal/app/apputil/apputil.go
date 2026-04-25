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
