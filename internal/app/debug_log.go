package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/logcontrol"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	debugLogRecentLimit   = 200
	debugLogCardMaxChars  = 12000
	debugLogPreviewAction = "menu.debug.logs"
)

func runtimeLogLevelText() string {
	return logcontrol.CurrentName()
}

func (a *App) setRuntimeDebug(enabled bool) string {
	level := logcontrol.SetDebug(enabled)
	if a != nil && a.cfg != nil {
		a.cfg.Log.Level = level
	}
	return level
}

func desiredDebugEnabled(args []string) (bool, error) {
	if len(args) == 0 {
		return !logcontrol.DebugEnabled(), nil
	}
	if len(args) > 1 {
		return false, fmt.Errorf("usage: /debug | /debug on | /debug off")
	}
	if enabled, ok := logcontrol.ToggleArgEnabled(args[0]); ok {
		return enabled, nil
	}
	return false, fmt.Errorf("usage: /debug | /debug on | /debug off")
}

func (a *App) commandDebug(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) > 0 && strings.TrimSpace(args[0]) == "logs" {
		return a.commandDebugLogs(msg, args[1:])
	}
	enabled, err := desiredDebugEnabled(args)
	if err != nil {
		return err
	}
	level := a.setRuntimeDebug(enabled)
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "服务端 slog 日志级别已切换为 `"+level+"`。", msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
}

func (a *App) completeMenuDebug(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	level := a.setRuntimeDebug(!logcontrol.DebugEnabled())
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已切换日志级别为 " + level},
		Card:  rawCard(a.renderSystemMenuCard(sessionKey)),
	}, nil
}

func (a *App) commandDebugLogs(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /debug logs")
	}
	card := a.renderDebugLogsCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, msg.ChatType == "group" && a.cfg.Feishu.ReplyInThread)
	return err
}

func (a *App) completeMenuDebugLogs(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开最近日志"},
		Card:  rawCard(a.renderDebugLogsCard(sessionKey)),
	}, nil
}

func (a *App) renderDebugLogsCard(sessionKey string) map[string]any {
	lines := logcontrol.RecentLines(debugLogRecentLimit)
	card := newMarkdownBodyCard("调试日志", "blue")
	var logBlock map[string]any
	summaryLines := []string{
		"当前位置：" + strings.Join(menuBreadcrumbLabels(debugLogPreviewAction), " / "),
		"",
		fmt.Sprintf("最近服务端 slog 日志（内存缓冲，最新 %d 条）。", debugLogRecentLimit),
		"当前日志级别: " + runtimeLogLevelText(),
	}
	if len(lines) == 0 {
		summaryLines = append(summaryLines, "", "当前还没有可展示的日志。")
	} else {
		logText, shown, truncated := compactDebugLogText(lines, debugLogCardMaxChars)
		switch {
		case truncated:
			summaryLines = append(summaryLines, fmt.Sprintf("显示范围: 最新 %d/%d 条", shown, len(lines)))
			summaryLines = append(summaryLines, "说明: 卡片内容过长，已截断为最新尾部。")
		default:
			summaryLines = append(summaryLines, fmt.Sprintf("显示范围: %d 条", shown))
		}
		logBlock = debugLogPlainTextBlock(logText, false)
	}
	appendMarkdownBodyCardElement(card, debugLogPlainTextBlock(strings.Join(summaryLines, "\n"), true))
	if logBlock != nil {
		appendMarkdownBodyCardElement(card, logBlock)
	}

	buttons := []feishu.Button{
		{Text: commandLabel("切换日志级别", "/debug"), Type: "default", Value: map[string]any{"action": "menu.debug", "session_key": sessionKey}},
		{Text: commandLabel("刷新日志", "/debug logs"), Type: "default", Value: map[string]any{"action": "menu.debug.logs", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	for _, btn := range buttons {
		appendMarkdownBodyCardElement(card, buildMarkdownBodyCardActionElement([]feishu.Button{btn}))
	}
	return card
}

func renderRuntimeLogLevelValue() string {
	level := strings.TrimSpace(runtimeLogLevelText())
	if level == "" {
		level = "info"
	}
	return "`" + level + "`"
}

func compactDebugLogText(lines []string, maxChars int) (string, int, bool) {
	if len(lines) == 0 {
		return "", 0, false
	}
	if maxChars <= 0 {
		return strings.Join(lines, "\n"), len(lines), false
	}
	selected := make([]string, 0, len(lines))
	size := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		extra := len(line)
		if len(selected) > 0 {
			extra++
		}
		if size+extra > maxChars && len(selected) > 0 {
			break
		}
		if size+extra > maxChars {
			if maxChars > 3 && len(line) > maxChars-3 {
				line = "..." + line[len(line)-(maxChars-3):]
			}
			selected = append(selected, line)
			break
		}
		selected = append(selected, line)
		size += extra
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return strings.Join(selected, "\n"), len(selected), len(selected) < len(lines)
}

func debugLogPlainTextBlock(content string, subtle bool) map[string]any {
	text := map[string]any{
		"tag":     "plain_text",
		"content": strings.TrimSpace(content),
	}
	if subtle {
		text["text_size"] = "notation"
		text["text_color"] = "grey"
	}
	return map[string]any{
		"tag":  "div",
		"text": text,
	}
}
