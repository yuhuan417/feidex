package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/feishu"
)

func (a *App) handleCommand(msg *feishu.InboundMessage, raw string) error {
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	spec := findLocalCommandSpec(fields[0])
	if spec == nil {
		return fmt.Errorf("unknown command: %s", fields[0])
	}
	if !a.hasConfiguredBackend() && fields[0] != "/backend" {
		return a.replyBackendSelectionCard(msg, "")
	}
	backend := a.configuredBackend()
	if backend == backendCodex {
		if err := a.codexMaintenanceBlocksCommand(raw); err != nil {
			return err
		}
	}
	if backend == backendClaude {
		if err := a.claudeMaintenanceBlocksCommand(raw); err != nil {
			return err
		}
	}
	if !commandHandlesLocallyForBackend(spec, backend, fields) {
		return a.enqueuePassthroughCommand(msg, raw)
	}
	return spec.Handle(a, msg, fields[1:])
}

func (a *App) enqueuePassthroughCommand(msg *feishu.InboundMessage, raw string) error {
	if a == nil || msg == nil {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	cloned := *msg
	cloned.Text = raw
	return a.enqueueSubmission(&cloned)
}

func isLocalCommandForBackend(backend, raw string) bool {
	raw = strings.TrimSpace(raw)
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return false
	}
	spec := findLocalCommandSpec(fields[0])
	if spec == nil {
		return false
	}
	return commandHandlesLocallyForBackend(spec, backend, fields)
}

func isLocalCommand(raw string) bool {
	return isLocalCommandForBackend(backendCodex, raw)
}

func (a *App) commandHelp(msg *feishu.InboundMessage, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: /help")
	}
	card := a.renderHelpCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}

func (a *App) renderToolsMenuCard(sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.tools")
	return a.feishu.SimpleStatusCard(spec.Label, "blue", menuCardBody(spec.Action, spec.Description), renderGroupMenuButtons(a.configuredBackend(), spec.Action, sessionKey))
}

func (a *App) renderSessionMenuCard(sessionKey string) map[string]any {
	return a.renderToolsMenuCard(sessionKey)
}

func (a *App) renderContextMenuCard(sessionKey string) map[string]any {
	return a.renderCommandMenuCard(sessionKey)
}

func (a *App) renderModelMenuCard(sessionKey string) map[string]any {
	if a.isClaudeBackend() {
		modelValue := firstNonEmpty(configuredClaudeModel(a.cfg), claudeDefaultModelAlias)
		effortValue := firstNonEmpty(configuredClaudeEffort(a.cfg), "(default)")
		body := strings.Join([]string{
			"当前 model: `" + modelValue + "`",
			"当前 effort: `" + effortValue + "`",
			"Claude model / effort 只允许在 frontend 空闲时切换。",
			"切换成功后会立即重置当前 frontend 的 Claude 会话。",
		}, "\n")
		buttons := []feishu.Button{
			{Text: submenuCommandLabel("模型配置", "/model"), Type: "default", Value: map[string]any{"action": "menu.model", "session_key": sessionKey}},
			{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}},
		}
		return a.feishu.SimpleStatusCard("模型配置", "blue", menuCardBody("menu.group.model", body), buttons)
	}
	modelValue := firstNonEmpty(configuredGlobalModel(a.cfg), "(default)")
	effortValue := firstNonEmpty(configuredGlobalReasoningEffort(a.cfg), "(default)")
	fastValue := "-"
	if a.store != nil {
		if sess := a.appState().session(sessionKey); sess != nil {
			fastValue = renderServiceTierValue(sess.ActiveThreadServiceTier)
		}
	}
	body := strings.Join([]string{
		"当前 model: `" + modelValue + "`",
		"当前 reasoning: `" + effortValue + "`",
		"当前 fast: " + fastValue,
	}, "\n")
	buttons := []feishu.Button{
		{Text: submenuCommandLabel("模型配置", "/model"), Type: "default", Value: map[string]any{"action": "menu.model", "session_key": sessionKey}},
		{Text: submenuCommandLabel("响应速度", "/fast config"), Type: "default", Value: map[string]any{"action": "menu.fast", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("模型配置", "blue", menuCardBody("menu.group.model", body), buttons)
}

func (a *App) renderSystemMenuCard(sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.group.system")
	backend := firstNonEmpty(a.configuredBackend(), "unset")
	body := spec.Description + "\n\n当前 backend: `" + backend + "`\n当前 slog 日志级别: " + renderRuntimeLogLevelValue() + "\n当前版本: `" + currentVersion() + "`"
	return a.feishu.SimpleStatusCard(spec.Label, "blue", menuCardBody(spec.Action, body), renderGroupMenuButtons(a.configuredBackend(), spec.Action, sessionKey))
}

func (a *App) renderHelpCard(sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("帮助说明", "blue", menuCardBody("menu.help", renderHelpBodyFromRegistry(a.configuredBackend())), buttons)
}
