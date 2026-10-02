package presentation

import (
	domainsubmission "feidex/internal/domain/submission"
	"strings"
)

// CompletionTerminalText returns the terminal text to display when a turn
// completes. Returns "" for successful completions.
func CompletionTerminalText(status, lastError string) string {
	lastError = strings.TrimSpace(lastError)
	if domainsubmission.NormalizeSubmissionStatus(status) == domainsubmission.SubmissionStatusCompleted {
		return ""
	}

	fallback := lastError
	if fallback == "" {
		switch status {
		case domainsubmission.SubmissionStatusInterrupted.String():
			fallback = "任务已中断。"
		case domainsubmission.SubmissionStatusFailed.String():
			fallback = "任务失败。"
		default:
			fallback = "任务已结束。"
		}
	}

	switch status {
	case domainsubmission.SubmissionStatusInterrupted.String():
		return "任务已中断。"
	default:
		return fallback
	}
}
