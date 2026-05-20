package quietmode

import (
	"strings"

	"feidex/internal/app/turnitem"
	"feidex/internal/config"
)

// Option describes a single quiet mode choice for menu rendering.
type Option struct {
	Mode        config.QuietMode
	Title       string
	Description string
}

// Options is the ordered list of quiet mode choices for menu cards.
var Options = []Option{
	{
		Mode:        config.QuietModeVerbose,
		Title:       "verbose",
		Description: "完整展开所有过程消息。",
	},
	{
		Mode:        config.QuietModeProgress,
		Title:       "progress",
		Description: "把两次 plan / agent message 之间的过程折叠成一张持续更新的 `工作中` 卡。",
	},
	{
		Mode:        config.QuietModeNormal,
		Title:       "normal",
		Description: "只发送 plan 和 agent / final message，不显示 `工作中` 卡。",
	},
	{
		Mode:        config.QuietModeFinal,
		Title:       "final",
		Description: "只保留最终答复。",
	},
}

// Mode returns the parsed quiet mode from the Feishu config, defaulting to
// progress on nil or invalid config.
func Mode(cfg *config.FeishuConfig) config.QuietMode {
	if cfg == nil {
		return config.QuietModeProgress
	}
	mode, err := config.ParseQuietMode(cfg.Quiet)
	if err != nil {
		return config.QuietModeProgress
	}
	return mode
}

// Enabled returns true when quiet mode is anything other than verbose.
func Enabled(cfg *config.FeishuConfig) bool {
	return Mode(cfg) != config.QuietModeVerbose
}

// WorkingCardEnabled returns true when quiet mode is set to progress.
func WorkingCardEnabled(cfg *config.FeishuConfig) bool {
	return Mode(cfg) == config.QuietModeProgress
}

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
	if IsMCPToolPayload(itemType, protocolItemType, toolName) {
		return mode == config.QuietModeNormal
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

func IsMCPToolPayload(itemType, protocolItemType, toolName string) bool {
	switch turnitem.NormalizeTurnItemType(protocolItemType) {
	case "mcp_tool_call":
		return true
	case "dynamic_tool_call":
		if turnitem.ClassifyDynamicTool(strings.TrimSpace(toolName)) == turnitem.DynamicToolMCPCategory {
			return true
		}
	}
	return turnitem.NormalizeTurnItemType(itemType) == "mcp_tool_call"
}
