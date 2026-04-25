package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/logcontrol"
	appcards "feidex/internal/app/cards"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type debugService struct {
	app *App
}
func newDebugService(app *App) debugService {
	return debugService{app: app}
}

const (
	debugLogRecentLimit   = 200
	debugLogCardMaxChars  = 12000
	debugLogPreviewAction = "menu.debug.logs"
)

const debugAccessUnauthorizedText = "当前用户无权使用 debug 功能"

func runtimeLogLevelText() string {
	return logcontrol.CurrentName()
}

func (s debugService) setRuntimeDebug(enabled bool) string {
	level := logcontrol.SetDebug(enabled)
	if s.app != nil && s.app.cfg != nil {
		s.app.configMu.Lock()
		s.app.cfg.Log.Level = level
		s.app.configMu.Unlock()
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

func (s debugService) commandDebug(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) > 0 && strings.TrimSpace(args[0]) == "logs" {
		return newDebugService(s.app).commandDebugLogs(msg, args[1:])
	}
	if !newDebugService(s.app).debugAccessAllowed(msg.UserID) {
		card := newDebugService(s.app).renderDebugAccessDeniedCard(makeSessionKey(s.app, msg), msg.UserID)
		_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	enabled, err := desiredDebugEnabled(args)
	if err != nil {
		return err
	}
	level := newDebugService(s.app).setRuntimeDebug(enabled)
	return s.app.feishu.ReplyText(context.Background(), msg.MessageID, "服务端 slog 日志级别已切换为 `"+level+"`。", replyInThreadEnabled(s.app, msg.ChatType))
}

func (s debugService) completeMenuDebug(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/debug", "menu.group.system")
}

func (s debugService) commandDebugLogs(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /debug logs")
	}
	if msg == nil {
		return nil
	}
	if !newDebugService(s.app).debugAccessAllowed(msg.UserID) {
		card := newDebugService(s.app).renderDebugAccessDeniedCard(makeSessionKey(s.app, msg), msg.UserID)
		_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
		return err
	}
	card := newDebugService(s.app).renderDebugLogsCard(makeSessionKey(s.app, msg))
	_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}

func (s debugService) completeMenuDebugLogs(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(s.app, action, sessionKey, "/debug logs", "menu.group.system")
}

func (s debugService) debugAccessAllowed(userID string) bool {
	if s.app == nil || s.app.cfg == nil {
		return false
	}
	return debugUserAllowed(userID, debugAllowFrom(s.app))
}

func debugUserAllowed(userID string, allowFrom []string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	for _, item := range allowFrom {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == "*" || item == userID {
			return true
		}
	}
	return false
}

func (s debugService) renderDebugAccessDeniedCard(sessionKey, userID string) map[string]any {
	bodyLines := []string{
		"当前用户无权使用 debug 功能。",
		"",
		"当前用户 OpenID: `" + firstNonEmpty(strings.TrimSpace(userID), "-") + "`",
	}
	if cfgPath := strings.TrimSpace(s.app.cfgPath); cfgPath != "" {
		bodyLines = append(bodyLines, "配置文件: `"+cfgPath+"`")
	}
	bodyLines = append(bodyLines,
		"",
		"请把该用户加入 `[feishu].debug_allow_from`，然后重启服务。",
		"",
		"示例配置：",
		markdownCodeBlockWithLang("toml", strings.Join([]string{
			"[feishu]",
			"debug_allow_from = [\"" + firstNonEmpty(strings.TrimSpace(userID), "ou_xxx") + "\"]",
		}, "\n")),
	)
	return s.app.feishu.SimpleStatusCard("Debug 权限不足", "orange", strings.Join(bodyLines, "\n"), []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	})
}

func actionUserID(action *feishu.CardAction) string {
	if action == nil {
		return ""
	}
	return strings.TrimSpace(action.UserID)
}

func (s debugService) renderDebugLogsCard(sessionKey string) map[string]any {
	lines := logcontrol.RecentLines(debugLogRecentLimit)
	card := appcards.NewMarkdownBodyCard("调试日志", "blue")
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
	appcards.AppendMarkdownBodyCardElement(card, debugLogPlainTextBlock(strings.Join(summaryLines, "\n"), true))
	if logBlock != nil {
		appcards.AppendMarkdownBodyCardElement(card, logBlock)
	}

	buttons := []feishu.Button{
		{Text: commandLabel("刷新日志", "/debug logs"), Type: "default", Value: map[string]any{"action": "menu.debug.logs", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	for _, btn := range buttons {
		appcards.AppendMarkdownBodyCardElement(card, appcards.BuildMarkdownBodyCardActionElement([]feishu.Button{btn}))
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
