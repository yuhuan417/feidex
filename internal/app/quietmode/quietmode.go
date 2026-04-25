package quietmode

import (
	"strings"

	"feidex/internal/app/turnitem"
	"feidex/internal/config"
)

func StatusText(mode config.QuietMode) string {
	return mode.String()
}

func ShouldDeliverTurnKind(mode config.QuietMode, kind string) bool {
	switch mode {
	case config.QuietModeProgress:
		switch strings.TrimSpace(kind) {
		case "final_message", "turn_output", "turn_plan", "turn_queued", "turn_started", "turn_terminal":
			return true
		default:
			return false
		}
	case config.QuietModeNormal:
		switch strings.TrimSpace(kind) {
		case "final_message", "turn_output", "turn_plan", "turn_started":
			return true
		default:
			return false
		}
	case config.QuietModeFinal:
		return strings.TrimSpace(kind) == "final_message"
	default:
		return true
	}
}

func ShouldDeliverTurnItem(mode config.QuietMode, itemType string, isFinalAnswer bool) bool {
	switch mode {
	case config.QuietModeProgress, config.QuietModeNormal:
		switch turnitem.NormalizeTurnItemType(itemType) {
		case "plan", "agent_message", "exited_review_mode":
			return true
		default:
			return false
		}
	case config.QuietModeFinal:
		switch turnitem.NormalizeTurnItemType(itemType) {
		case "agent_message", "exited_review_mode":
			return isFinalAnswer
		default:
			return false
		}
	default:
		return true
	}
}

func ShouldDeliverTurnItemPayload(mode config.QuietMode, itemType, protocolItemType, toolName string, isFinalAnswer bool) bool {
	if ShouldDeliverTurnItem(mode, itemType, isFinalAnswer) {
		return true
	}
	if !IsClaudeTodoToolPayload(protocolItemType, toolName) {
		return false
	}
	switch mode {
	case config.QuietModeProgress, config.QuietModeNormal:
		return true
	default:
		return false
	}
}

func IsClaudeTodoToolPayload(protocolItemType, toolName string) bool {
	return turnitem.NormalizeTurnItemType(protocolItemType) == "dynamic_tool_call" && strings.TrimSpace(toolName) == "TodoWrite"
}

