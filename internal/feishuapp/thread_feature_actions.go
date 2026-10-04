package feishuapp

import (
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/textutil"
	"strings"

	"feidex/internal/feishu"
)

func interruptStatusButtons(sessionKey, parentAction, targetTurnID string, includeRetry bool) []feishu.Button {
	buttons := []feishu.Button{}
	if includeRetry {
		value := map[string]any{
			"action":      "menu.interrupt",
			"session_key": sessionKey,
		}
		if strings.TrimSpace(parentAction) != "" {
			value["parent_action"] = strings.TrimSpace(parentAction)
		}
		if strings.TrimSpace(targetTurnID) != "" {
			value["turn_id"] = strings.TrimSpace(targetTurnID)
		}
		buttons = append(buttons, feishu.Button{
			Text:  "重试",
			Type:  "primary",
			Value: value,
		})
	}
	backAction := textutil.FirstNonEmpty(strings.TrimSpace(parentAction), "menu.tools")
	buttons = append(buttons, feishu.Button{
		Text: feishu.MenuBackButtonText,
		Type: "default",
		Value: map[string]any{
			"action":      backAction,
			"session_key": sessionKey,
		},
	})
	return buttons
}

func renderInterruptPreparingCard(state planmode.SessionStateProvider, client FeishuClient, sessionKey, parentAction string) map[string]any {
	return client.SimpleStatusCard(
		planmode.ContentCardTitleForSessionFromState(state, state != nil, sessionKey, "", "中断任务"),
		"blue",
		menuCardBody(textutil.FirstNonEmpty(strings.TrimSpace(parentAction), "menu.tools"), "正在向 Claude 请求中断当前任务，请稍候。\n\n这张卡片会自动刷新。"),
		nil,
	)
}

func renderInterruptResultCard(state planmode.SessionStateProvider, client FeishuClient, sessionKey, parentAction, text string) map[string]any {
	return client.SimpleStatusCard(
		planmode.ContentCardTitleForSessionFromState(state, state != nil, sessionKey, "", "中断任务"),
		"green",
		menuCardBody(textutil.FirstNonEmpty(strings.TrimSpace(parentAction), "menu.tools"), textutil.FirstNonEmpty(strings.TrimSpace(text), "已请求中断当前任务。")),
		interruptStatusButtons(sessionKey, parentAction, "", false),
	)
}

func renderInterruptFailedCard(state planmode.SessionStateProvider, client FeishuClient, sessionKey, parentAction, targetTurnID, errText string) map[string]any {
	body := "请求中断当前任务失败。"
	if text := strings.TrimSpace(errText); text != "" {
		body += "\n\n错误: " + text
	}
	return client.SimpleStatusCard(
		planmode.ContentCardTitleForSessionFromState(state, state != nil, sessionKey, "", "中断任务"),
		"orange",
		menuCardBody(textutil.FirstNonEmpty(strings.TrimSpace(parentAction), "menu.tools"), body),
		interruptStatusButtons(sessionKey, parentAction, targetTurnID, true),
	)
}
