package quietmode

import (
	"strings"

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
		switch normalizeTurnItemType(itemType) {
		case "plan", "agent_message", "exited_review_mode":
			return true
		default:
			return false
		}
	case config.QuietModeFinal:
		switch normalizeTurnItemType(itemType) {
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
	return normalizeTurnItemType(protocolItemType) == "dynamic_tool_call" && strings.TrimSpace(toolName) == "TodoWrite"
}

func normalizeTurnItemType(itemType string) string {
	itemType = strings.TrimSpace(itemType)
	itemType = strings.ReplaceAll(itemType, "HookPrompt", "hook_prompt")
	itemType = strings.ReplaceAll(itemType, "WebSearch", "web_search")
	itemType = strings.ReplaceAll(itemType, "ImageView", "image_view")
	itemType = strings.ReplaceAll(itemType, "ImageGeneration", "image_generation")
	itemType = strings.ReplaceAll(itemType, "EnteredReviewMode", "entered_review_mode")
	itemType = strings.ReplaceAll(itemType, "ExitedReviewMode", "exited_review_mode")
	itemType = strings.ReplaceAll(itemType, "ContextCompaction", "context_compaction")
	itemType = strings.ReplaceAll(itemType, "McpToolCall", "mcp_tool_call")
	itemType = strings.ReplaceAll(itemType, "DynamicToolCall", "dynamic_tool_call")
	itemType = strings.ReplaceAll(itemType, "CollabAgentToolCall", "collab_agent_tool_call")
	itemType = strings.ReplaceAll(itemType, "UserMessage", "user_message")
	itemType = strings.ReplaceAll(itemType, "AgentMessage", "agent_message")
	itemType = strings.ReplaceAll(itemType, "CommandExecution", "command_execution")
	itemType = strings.ReplaceAll(itemType, "FileChange", "file_change")
	itemType = strings.ReplaceAll(itemType, "hookPrompt", "hook_prompt")
	itemType = strings.ReplaceAll(itemType, "webSearch", "web_search")
	itemType = strings.ReplaceAll(itemType, "imageView", "image_view")
	itemType = strings.ReplaceAll(itemType, "imageGeneration", "image_generation")
	itemType = strings.ReplaceAll(itemType, "enteredReviewMode", "entered_review_mode")
	itemType = strings.ReplaceAll(itemType, "exitedReviewMode", "exited_review_mode")
	itemType = strings.ReplaceAll(itemType, "contextCompaction", "context_compaction")
	itemType = strings.ReplaceAll(itemType, "mcpToolCall", "mcp_tool_call")
	itemType = strings.ReplaceAll(itemType, "dynamicToolCall", "dynamic_tool_call")
	itemType = strings.ReplaceAll(itemType, "collabAgentToolCall", "collab_agent_tool_call")
	itemType = strings.ReplaceAll(itemType, "userMessage", "user_message")
	itemType = strings.ReplaceAll(itemType, "agentMessage", "agent_message")
	itemType = strings.ReplaceAll(itemType, "commandExecution", "command_execution")
	itemType = strings.ReplaceAll(itemType, "fileChange", "file_change")
	return strings.ToLower(strings.TrimSpace(itemType))
}
