package compaction

import (
	"feidex/internal/adapter/feishu/menuutil"
	"feidex/internal/feishu"
	"strings"
)

// CompactMenuButtons builds the buttons for compact result cards.
func CompactMenuButtons(sessionKey string, includeRetry bool) []feishu.Button {
	buttons := []feishu.Button{}
	if includeRetry {
		buttons = append(buttons, feishu.Button{
			Text: "重试",
			Type: "primary",
			Value: map[string]any{
				"action":        "menu.compact",
				"session_key":   sessionKey,
				"parent_action": "menu.tools",
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: feishu.MenuBackButtonText,
		Type: "default",
		Value: map[string]any{
			"action":      "menu.tools",
			"session_key": sessionKey,
		},
	})
	return buttons
}

// RenderCompactPreparingCard builds the "preparing" card for compact action.
func RenderCompactPreparingCard(title, sessionKey string) map[string]any {
	body := "正在请求当前线程上下文压缩，请稍候。\n\n这张卡片会自动刷新。"
	return feishu.SimpleStatusCard(title, "blue", menuutil.MenuCardBody("menu.tools", body), CompactMenuButtons(sessionKey, false))
}

// RenderCompactAcceptedCard builds the "accepted" card for compact action.
func RenderCompactAcceptedCard(title, sessionKey string) map[string]any {
	body := "已提交 `/compact`。\n\n后续结果会通过正常消息流返回。"
	return feishu.SimpleStatusCard(title, "green", menuutil.MenuCardBody("menu.tools", body), CompactMenuButtons(sessionKey, false))
}

// RenderCompactFailedCard builds the "failed" card for compact action.
func RenderCompactFailedCard(title, sessionKey, errText string) map[string]any {
	body := "请求 `/compact` 失败。"
	if text := strings.TrimSpace(errText); text != "" {
		body += "\n\n错误: " + text
	}
	return feishu.SimpleStatusCard(title, "orange", menuutil.MenuCardBody("menu.tools", body), CompactMenuButtons(sessionKey, true))
}
