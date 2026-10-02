package claude

import (
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/textutil"
	"fmt"
	"strings"
)

func AttachmentPrompt(attachment domainsubmission.SubmissionAttachment) string {
	path := strings.TrimSpace(attachment.LocalPath)
	if path == "" {
		return ""
	}
	switch attachment.Kind {
	case "file":
		return fmt.Sprintf("User attached file: %s", path)
	case "image":
		return fmt.Sprintf("User attached image: %s", path)
	case "audio":
		return fmt.Sprintf("User attached audio file (not transcribed): %s", path)
	case "media":
		return fmt.Sprintf("User attached video file: %s", path)
	default:
		return fmt.Sprintf("User attached %s: %s", strings.TrimSpace(attachment.Kind), path)
	}
}

// BuildPrompt builds the prompt text for a Claude submission from
// skills, input text, and attachments.
func BuildPrompt(sub *domainsubmission.Submission) string {
	if sub == nil {
		return ""
	}
	parts := make([]string, 0, len(sub.Skills)+1+len(sub.Attachments))
	for _, skill := range sub.Skills {
		if strings.TrimSpace(skill.Name) == "" && strings.TrimSpace(skill.Path) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("Use skill `%s` (`%s`) if it is available in this Claude session.", textutil.FirstNonEmpty(strings.TrimSpace(skill.Name), "skill"), textutil.FirstNonEmpty(strings.TrimSpace(skill.Path), "-")))
	}
	if text := strings.TrimSpace(sub.InputText); text != "" {
		parts = append(parts, text)
	}
	for _, attachment := range sub.Attachments {
		if prompt := AttachmentPrompt(attachment); prompt != "" {
			parts = append(parts, prompt)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}
