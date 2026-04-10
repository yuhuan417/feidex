package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/logcontrol"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
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
	lines := logcontrol.RecentLines(200)
	body := []string{
		"最近服务端 slog 日志（内存缓冲，最新 200 条）。",
		"",
		"当前日志级别: " + renderRuntimeLogLevelValue(),
	}
	if len(lines) == 0 {
		body = append(body, "", "当前还没有可展示的日志。")
	} else {
		content := strings.Join(lines, "\n")
		if len(content) > 6000 {
			content = "...\n" + content[len(content)-6000:]
		}
		body = append(body, "", "```text\n"+normalizeCardMarkdown(content)+"\n```")
	}
	buttons := []feishu.Button{
		{Text: commandLabel("切换日志级别", "/debug"), Type: "default", Value: map[string]any{"action": "menu.debug", "session_key": sessionKey}},
		{Text: commandLabel("刷新日志", "/debug logs"), Type: "default", Value: map[string]any{"action": "menu.debug.logs", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("调试日志", "blue", menuCardBody("menu.debug.logs", strings.Join(body, "\n")), buttons)
}

func renderRuntimeLogLevelValue() string {
	level := strings.TrimSpace(runtimeLogLevelText())
	if level == "" {
		level = "info"
	}
	return "`" + level + "`"
}
