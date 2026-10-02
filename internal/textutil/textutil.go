// Package textutil contains dependency-free text helpers shared by adapters
// and application packages.
package textutil

import "strings"

// FirstNonEmpty returns the first string from values that is non-empty after
// trimming whitespace. If all values are empty, it returns "".
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// Truncate truncates value to at most limit runes, appending "…" when it was
// shortened. If limit <= 0, value is returned unchanged.
func Truncate(value string, limit int) string {
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}
