// Package submission provides pure helper functions for working with
// submissions, staged images, and turn completion status. These functions
// have no dependency on *App or service structs.
package submission

import (
	"strings"

	"feidex/internal/app/apputil"
	"feidex/internal/state"
)

// UniqueStrings returns unique non-empty strings after trimming whitespace,
// preserving order.
func UniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// RemoveString removes the first occurrence of target (after trimming) from
// values, returning a new slice.
func RemoveString(values []string, target string) []string {
	target = strings.TrimSpace(target)
	if target == "" {
		return append([]string(nil), values...)
	}
	out := make([]string, 0, len(values))
	removed := false
	for _, value := range values {
		if !removed && strings.TrimSpace(value) == target {
			removed = true
			continue
		}
		out = append(out, value)
	}
	return out
}

// StagedImageAttachments converts staged images to submission attachments,
// skipping any images without a local path.
func StagedImageAttachments(images []state.SessionStagedImage) []state.SubmissionAttachment {
	if len(images) == 0 {
		return nil
	}
	attachments := make([]state.SubmissionAttachment, 0, len(images))
	for _, image := range images {
		if strings.TrimSpace(image.LocalPath) == "" {
			continue
		}
		attachments = append(attachments, state.SubmissionAttachment{
			Kind:      "image",
			Name:      image.Name,
			LocalPath: image.LocalPath,
		})
	}
	return attachments
}

// StagedImageSourceMessageIDs extracts unique source message IDs from
// staged images.
func StagedImageSourceMessageIDs(images []state.SessionStagedImage) []string {
	if len(images) == 0 {
		return nil
	}
	ids := make([]string, 0, len(images))
	for _, image := range images {
		ids = append(ids, image.SourceMessageID)
	}
	return UniqueStrings(ids)
}

// StagedImageRootMessageIDs extracts unique root message IDs from staged
// images, falling back to source message IDs.
func StagedImageRootMessageIDs(images []state.SessionStagedImage) []string {
	if len(images) == 0 {
		return nil
	}
	ids := make([]string, 0, len(images))
	for _, image := range images {
		rootID := apputil.FirstNonEmpty(strings.TrimSpace(image.RootMessageID), strings.TrimSpace(image.SourceMessageID))
		if rootID == "" {
			continue
		}
		ids = append(ids, rootID)
	}
	return UniqueStrings(ids)
}

// HasSourceMessage checks if a submission has the given message ID among its
// source or trigger message IDs.
func HasSourceMessage(sub *state.Submission, messageID string) bool {
	if sub == nil {
		return false
	}
	for _, candidate := range SourceMessageIDs(sub) {
		if candidate == messageID {
			return true
		}
	}
	return false
}

// SourceMessageIDs returns all unique source message IDs for a submission,
// including the trigger message ID.
func SourceMessageIDs(sub *state.Submission) []string {
	if sub == nil {
		return nil
	}
	ids := append([]string{}, sub.SourceMessageIDs...)
	if strings.TrimSpace(sub.TriggerMessageID) != "" {
		ids = append(ids, sub.TriggerMessageID)
	}
	return UniqueStrings(ids)
}

// CompletionTerminalText returns the terminal text to display when a turn
// completes. Returns "" for successful completions.
func CompletionTerminalText(status, lastError string) string {
	lastError = strings.TrimSpace(lastError)
	if status == "completed" {
		return ""
	}

	fallback := lastError
	if fallback == "" {
		switch status {
		case "interrupted":
			fallback = "任务已中断。"
		case "failed":
			fallback = "任务失败。"
		default:
			fallback = "任务已结束。"
		}
	}

	switch status {
	case "interrupted":
		return "任务已中断。"
	default:
		return fallback
	}
}
