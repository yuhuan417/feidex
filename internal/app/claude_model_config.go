package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	claudeDefaultModelAlias = "sonnet"
	effortCommandUsage      = "/effort | /effort <effort|default>"
)

type claudeModelOption struct {
	Value string
	Label string
}

var claudeBuiltinModelOptions = []claudeModelOption{
	{Value: "sonnet", Label: "Sonnet (`sonnet`)"},
	{Value: "opus", Label: "Opus (`opus`)"},
	{Value: "haiku", Label: "Haiku (`haiku`)"},
}

func configuredClaudeModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Claude.Model)
}

func configuredClaudeEffort(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Claude.Effort)
}

func normalizeClaudeModelValue(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "", "default", modelConfigDefaultOptionValue:
		return claudeDefaultModelAlias
	default:
		return value
	}
}

func claudeModelPickerOptions(cfg *config.Config) []claudeModelOption {
	options := make([]claudeModelOption, 0, len(claudeBuiltinModelOptions)+1)
	seen := map[string]struct{}{}
	current := configuredClaudeModel(cfg)
	for _, item := range claudeBuiltinModelOptions {
		label := item.Label
		if item.Value == current {
			label = "当前 · " + label
		}
		options = append(options, claudeModelOption{
			Value: item.Value,
			Label: label,
		})
		seen[item.Value] = struct{}{}
	}
	if current != "" {
		if _, ok := seen[current]; !ok {
			options = append(options, claudeModelOption{
				Value: current,
				Label: "当前 · 自定义 (`" + current + "`)",
			})
		}
	}
	return options
}

func (a *App) renderClaudeModelConfigCard(sessionKey, menuAction string) map[string]any {
	menuAction = strings.TrimSpace(menuAction)
	if menuAction == "" {
		menuAction = "menu.model"
	}
	currentModel := firstNonEmpty(configuredClaudeModel(a.cfg), claudeDefaultModelAlias)
	currentEffort := firstNonEmpty(configuredClaudeEffort(a.cfg), "(default)")

	elements := []map[string]any{
		{
			"tag": "markdown",
			"content": "当前 backend: `claude`\n" +
				"当前模型: `" + currentModel + "`\n" +
				"当前推理强度: `" + currentEffort + "`\n\n" +
				"这里提供 Claude 常用别名与当前自定义 model。\n" +
				"需要任意 raw model 时，请直接使用 `/model set <model-id>`。\n" +
				"`/model set default` 会恢复为 `sonnet`。\n" +
				"切换 Claude model / effort 只允许在当前 frontend 空闲时进行；成功后会立即重置当前 Claude 会话。",
		},
		{"tag": "markdown", "content": "选择 Claude 默认模型"},
	}

	modelOptions := make([]selectStaticOption, 0, len(claudeBuiltinModelOptions)+1)
	for _, item := range claudeModelPickerOptions(a.cfg) {
		modelOptions = append(modelOptions, selectStaticOption{
			Text:  item.Label,
			Value: item.Value,
		})
	}
	elements = append(elements, buildSelectStaticElement(
		"claude_model_config_select_model",
		"选择 Claude 默认模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": menuAction},
		modelOptions,
		currentModel,
	))

	effortValue := configuredClaudeEffort(a.cfg)
	effortOptions := []selectStaticOption{{
		Text: func() string {
			if effortValue == "" {
				return "当前 · 跟随默认"
			}
			return "跟随默认"
		}(),
		Value: modelConfigDefaultOptionValue,
	}}
	effortInitialOption := modelConfigDefaultOptionValue
	if effortValue != "" {
		effortInitialOption = effortValue
	}
	for _, effort := range config.SupportedClaudeEfforts() {
		label := effort
		if effort == effortValue && effortValue != "" {
			label = "当前 · " + label
		}
		effortOptions = append(effortOptions, selectStaticOption{
			Text:  label,
			Value: effort,
		})
	}
	elements = append(elements,
		map[string]any{"tag": "markdown", "content": "选择 Claude 推理强度"},
		buildSelectStaticElement(
			"claude_model_config_select_effort",
			"选择 Claude 推理强度",
			map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": menuAction},
			effortOptions,
			effortInitialOption,
		),
	)
	if strings.TrimSpace(sessionKey) != "" {
		elements = append(elements, modelCardActionRow([]feishu.Button{{
			Text:  "返回上一级",
			Type:  "default",
			Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey},
		}}))
	}

	card := newMarkdownBodyCard("模型配置", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBody(menuAction, "")})
	for _, elem := range elements {
		appendMarkdownBodyCardElement(card, elem)
	}
	return card
}

func (a *App) ensureClaudeRuntimeConfigChangeSafe() error {
	if a == nil {
		return fmt.Errorf("app not initialized")
	}
	if reason := strings.TrimSpace(a.frontendIdleBlockedReason()); reason != "" {
		return fmt.Errorf("Claude model / effort 只能在当前 frontend 空闲时切换: %s", reason)
	}
	return nil
}

func (a *App) resetClaudeFrontendSessionsForConfigChange() error {
	if a == nil || a.claude == nil {
		return nil
	}
	appState := a.appState()
	var firstErr error
	for _, sess := range appState.sessions() {
		if sess == nil || !a.sessionBelongsToFrontend(sess.Key) {
			continue
		}
		sessionKey := strings.TrimSpace(sess.Key)
		if sessionKey == "" {
			continue
		}
		if _, err := appState.updateSession(sessionKey, func(current *state.Session) {
			if current == nil {
				return
			}
			clearSessionThreadContext(current)
			sessionClearBackendThread(current, backendClaude)
			current.Status = firstNonEmpty(strings.TrimSpace(current.Status), "idle")
		}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		a.clearSessionLiveThread(sessionKey)
		if err := a.claude.ResetSession(sessionKey); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a *App) updateClaudeModelConfig(mutate func(*config.ClaudeConfig)) error {
	if a.cfg == nil {
		return fmt.Errorf("nil config")
	}
	if strings.TrimSpace(a.cfgPath) == "" {
		return fmt.Errorf("missing config path")
	}
	if err := a.ensureClaudeRuntimeConfigChangeSafe(); err != nil {
		return err
	}
	mutate(&a.cfg.Claude)
	if err := a.cfg.Normalize(filepath.Dir(a.cfgPath)); err != nil {
		return err
	}
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return err
	}
	if a.claude != nil {
		a.claude.UpdateConfig(a.cfg.Claude)
		if err := a.resetClaudeFrontendSessionsForConfigChange(); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) completeClaudeModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	if err := a.updateClaudeModelConfig(func(c *config.ClaudeConfig) {
		c.Model = normalizeClaudeModelValue(modelID)
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 Claude 模型，并重置当前 frontend 的 Claude 会话"},
		Card:  rawCard(a.renderClaudeModelConfigCard(sessionKey, menuAction)),
	}, nil
}

func (a *App) completeClaudeEffortSet(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	normalized, err := config.NormalizeClaudeEffort(effort)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	if err := a.updateClaudeModelConfig(func(c *config.ClaudeConfig) {
		c.Effort = normalized
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 Claude 推理强度，并重置当前 frontend 的 Claude 会话"},
		Card:  rawCard(a.renderClaudeModelConfigCard(sessionKey, menuAction)),
	}, nil
}

func (a *App) commandClaudeModel(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	sessionKey := a.makeSessionKey(msg)
	if len(args) > 0 {
		action := a.commandActionFromMessage(msg, map[string]any{
			"menu_action": "menu.model",
			"session_key": sessionKey,
		})
		switch strings.TrimSpace(args[0]) {
		case "set":
			if len(args) != 2 {
				return fmt.Errorf("usage: %s", modelCommandUsage)
			}
			resp, err := a.completeClaudeModelSet(action, strings.TrimSpace(args[1]))
			if err != nil {
				return err
			}
			return a.replyCommandActionResponse(msg, resp)
		case "effort":
			if len(args) != 2 {
				return fmt.Errorf("usage: %s", modelCommandUsage)
			}
			effort := strings.TrimSpace(args[1])
			if effort == "default" || effort == modelConfigDefaultOptionValue {
				effort = ""
			}
			resp, err := a.completeClaudeEffortSet(action, effort)
			if err != nil {
				return err
			}
			return a.replyCommandActionResponse(msg, resp)
		default:
			return fmt.Errorf("usage: %s", modelCommandUsage)
		}
	}
	card := a.renderClaudeModelConfigCard(sessionKey, "menu.model")
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}

func (a *App) commandEffort(msg *feishu.InboundMessage, args []string) error {
	switch len(args) {
	case 0:
		return a.commandModel(msg, nil)
	case 1:
		return a.commandModel(msg, []string{"effort", strings.TrimSpace(args[0])})
	default:
		return fmt.Errorf("usage: %s", effortCommandUsage)
	}
}
