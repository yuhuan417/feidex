package carddemo

import (
	"strings"

	appturnitem "feidex/internal/app/turnitem"
)

func NormalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "plain":
		return ""
	case "agent", "agent_message", "turn_output":
		return "turn_output"
	case "final", "final_agent", "final_message":
		return "final_message"
	case "turn_reasoning", "turn_command_execution", "turn_file_change", "turn_plan", "turn_queued", "turn_started", "turn_terminal":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return ""
	}
}

func DefaultBody(kind string) string {
	switch kind {
	case "final_message":
		return "这是 final agent message 卡片 demo。\n\n- 没有标题文案\n- 会保留绿色 header\n- 会走 final message 渲染路径"
	case "turn_output":
		return "这是普通 agent message 卡片 demo。\n\n它会复用当前 `turn_output` 的卡片样式。"
	case "turn_reasoning":
		return "这是 reasoning 卡片 demo。"
	case "turn_command_execution":
		return "命令执行:\n" + appturnitem.MarkdownCodeBlockWithLang("bash", "pwd")
	case "turn_file_change":
		return "文件改动:\n- [main.go](file:///tmp/main.go)"
	case "turn_plan":
		return "- [in_progress] 验证卡片样式\n- [pending] 调整细节"
	case "turn_queued":
		return "任务正在排队。"
	case "turn_started":
		return "任务已开始处理。"
	case "turn_terminal":
		return "任务已结束。"
	default:
		return "这是卡片 demo。"
	}
}
