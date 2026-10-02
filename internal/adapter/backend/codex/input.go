package codex

import (
	"feidex/internal/application/presentation"
	domainsubmission "feidex/internal/domain/submission"
	"strings"
)

func BuildTurnInputs(sub *domainsubmission.Submission) []map[string]any {
	inputs := make([]map[string]any, 0, len(sub.Skills)+1+len(sub.Attachments))
	for _, skill := range sub.Skills {
		if strings.TrimSpace(skill.Name) == "" || strings.TrimSpace(skill.Path) == "" {
			continue
		}
		inputs = append(inputs, map[string]any{
			"type": "skill",
			"name": skill.Name,
			"path": skill.Path,
		})
	}
	if text := strings.TrimSpace(sub.InputText); text != "" {
		inputs = append(inputs, TextInput(text))
	}
	for _, attachment := range sub.Attachments {
		switch attachment.Kind {
		case "image":
			if strings.TrimSpace(attachment.LocalPath) != "" {
				inputs = append(inputs, map[string]any{
					"type": "localImage",
					"path": attachment.LocalPath,
				})
			}
		case "file":
			if prompt := presentation.AttachmentPrompt(attachment); prompt != "" {
				inputs = append(inputs, TextInput(prompt))
			}
		default:
			if prompt := presentation.AttachmentPrompt(attachment); prompt != "" {
				inputs = append(inputs, TextInput(prompt))
			}
		}
	}
	return inputs
}

func TextInput(text string) map[string]any {
	return map[string]any{
		"type":          "text",
		"text":          text,
		"text_elements": []any{},
	}
}
