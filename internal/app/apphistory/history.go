package apphistory

import (
	"encoding/json"
	"fmt"
	"strings"

	"feidex/internal/app/appcore"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/codexrpc"
)

// TurnSummary is an alias for the runtime type.
type TurnSummary = appruntime.HistoryTurnSummary

// SummarizeThreadHistory builds turn summaries from thread read turns.
func SummarizeThreadHistory(turns []codexrpc.ThreadReadTurn, currentTurnID string) []TurnSummary {
	summaries := make([]TurnSummary, 0, len(turns))
	for idx, turn := range turns {
		summary := TurnSummary{
			Ordinal:   idx + 1,
			TurnID:    strings.TrimSpace(turn.ID),
			Status:    strings.TrimSpace(turn.Status),
			IsCurrent: strings.TrimSpace(turn.ID) != "" && strings.TrimSpace(turn.ID) == strings.TrimSpace(currentTurnID),
		}
		if turn.Error != nil {
			summary.ErrorText = strings.TrimSpace(appcore.FirstNonEmpty(turn.Error.Message, StringPtrValue(turn.Error.AdditionalDetails)))
		}
		for _, item := range turn.Items {
			switch strings.TrimSpace(item.Type) {
			case "userMessage":
				summary.Inputs = append(summary.Inputs, UserMessageInputs(item)...)
			case "agentMessage":
				if text := strings.TrimSpace(item.Text); text != "" {
					summary.Outputs = append(summary.Outputs, text)
				}
			}
		}
		summary.InputPreview = InputPreview(summary.Inputs)
		summaries = append(summaries, summary)
	}
	for i, j := 0, len(summaries)-1; i < j; i, j = i+1, j-1 {
		summaries[i], summaries[j] = summaries[j], summaries[i]
	}
	return summaries
}

// UserMessageInputs extracts user message inputs from a thread read item.
func UserMessageInputs(item codexrpc.ThreadReadItem) []string {
	if len(item.Content) == 0 {
		return nil
	}
	var inputs []codexrpc.ThreadReadUserInput
	if err := json.Unmarshal(item.Content, &inputs); err != nil {
		return nil
	}
	rendered := make([]string, 0, len(inputs))
	for _, input := range inputs {
		switch strings.TrimSpace(input.Type) {
		case "text":
			if text := strings.TrimSpace(input.Text); text != "" {
				rendered = append(rendered, text)
			}
		case "image":
			rendered = append(rendered, "[image] "+appcore.FirstNonEmpty(strings.TrimSpace(input.URL), "(no url)"))
		case "localImage":
			rendered = append(rendered, "[localImage] "+appcore.FirstNonEmpty(strings.TrimSpace(input.Path), "(no path)"))
		case "skill":
			rendered = append(rendered, "[skill] "+appcore.FirstNonEmpty(strings.TrimSpace(input.Name), strings.TrimSpace(input.Path), "(unknown skill)"))
		case "mention":
			rendered = append(rendered, "[mention] "+appcore.FirstNonEmpty(strings.TrimSpace(input.Name), strings.TrimSpace(input.Path), "(unknown mention)"))
		}
	}
	return rendered
}

// InputPreview returns a short preview of the inputs.
func InputPreview(inputs []string) string {
	if len(inputs) == 0 {
		return ""
	}
	if len(inputs) == 1 {
		return appcore.Truncate(inputs[0], 72)
	}
	return appcore.Truncate(inputs[0], 56) + fmt.Sprintf(" 等 %d 条", len(inputs))
}

// StringPtrValue dereferences a string pointer, returning "" if nil.
func StringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
