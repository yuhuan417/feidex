package presentation

import (
	domainsubmission "feidex/internal/domain/submission"
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
