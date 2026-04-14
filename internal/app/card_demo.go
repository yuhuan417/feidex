package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type OutboundCardDemoOptions struct {
	Kind        string
	WorkspaceID string
	ChatID      string
	UserID      string
	Body        string
}

func BuildOutboundCardDemo(cfg *config.Config, opts OutboundCardDemoOptions) (map[string]any, string, error) {
	if cfg == nil {
		return nil, "", fmt.Errorf("nil config")
	}

	kind := normalizeOutboundCardDemoKind(opts.Kind)
	if kind == "" {
		return nil, "", fmt.Errorf("unsupported outbound card demo kind %q", opts.Kind)
	}

	workspaceID := strings.TrimSpace(opts.WorkspaceID)
	if workspaceID == "" && len(cfg.Workspaces) > 0 {
		workspaceID = cfg.Workspaces[0].ID
	}
	body := strings.TrimSpace(opts.Body)
	if body == "" {
		body = defaultOutboundCardDemoBody(kind)
	}

	a := &App{
		cfg:    cfg,
		feishu: feishu.New(cfg.Feishu),
	}
	sub := &state.Submission{
		WorkspaceID: workspaceID,
		ChatID:      strings.TrimSpace(opts.ChatID),
		UserID:      strings.TrimSpace(opts.UserID),
	}

	title, color, replyClass, showHeader := outboundMessageCardMeta(kind)
	if replyClass {
		card := a.renderReplyMarkdownCardWithHeaderOptions(context.Background(), sub, title, color, showHeader, body, nil, kind == "final_message")
		return card, kind, nil
	}
	card := a.renderCompactMarkdownCard(sub, title, color, "", body, nil)
	return card, kind, nil
}

func normalizeOutboundCardDemoKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "plain":
		return ""
	case "agent", "agent_message", "turn_output":
		return "turn_output"
	case "final", "final_agent", "final_message":
		return "final_message"
	case "turn_reasoning", "turn_command_execution", "turn_file_change", "turn_plan", "turn_queued", "turn_terminal":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return ""
	}
}

func defaultOutboundCardDemoBody(kind string) string {
	switch kind {
	case "final_message":
		return "这是 final agent message 卡片 demo。\n\n- 没有标题文案\n- 会保留绿色 header\n- 会走 final message 渲染路径"
	case "turn_output":
		return "这是普通 agent message 卡片 demo。\n\n它会复用当前 `turn_output` 的卡片样式。"
	case "turn_reasoning":
		return "这是 reasoning 卡片 demo。"
	case "turn_command_execution":
		return "命令执行:\n" + markdownCodeBlockWithLang("bash", "pwd")
	case "turn_file_change":
		return "文件改动:\n- [main.go](file:///tmp/main.go)"
	case "turn_plan":
		return "- [in_progress] 验证卡片样式\n- [pending] 调整细节"
	case "turn_queued":
		return "任务正在排队。"
	case "turn_terminal":
		return "任务已结束。"
	default:
		return "这是卡片 demo。"
	}
}
