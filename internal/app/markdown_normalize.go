package app

import "strings"

const (
	markdownFenceMinLen       = 3
	markdownFencePreferredLen = 4
)

func normalizeCardMarkdown(text string) string {
	return strings.TrimSpace(text)
}

func parseBacktickFenceLine(trimmed string) (run int, info string, ok bool) {
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" || trimmed[0] != '`' {
		return 0, "", false
	}
	run = countLeadingBackticks(trimmed)
	if run < markdownFenceMinLen {
		return 0, "", false
	}
	info = strings.TrimSpace(trimmed[run:])
	return run, info, true
}

func countLeadingBackticks(s string) int {
	n := 0
	for n < len(s) && s[n] == '`' {
		n++
	}
	return n
}

func maxConsecutiveBackticks(s string) int {
	maxRun := 0
	run := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			run++
			if run > maxRun {
				maxRun = run
			}
			continue
		}
		run = 0
	}
	return maxRun
}
