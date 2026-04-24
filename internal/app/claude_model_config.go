package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/claudecli"
	"feidex/internal/config"
	"feidex/internal/feishu"

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

func (s modelConfigService) renderClaudeModelConfigCard(sessionKey, menuAction string) map[string]any {
	menuAction = strings.TrimSpace(menuAction)
	if menuAction == "" {
		menuAction = "menu.model"
	}
	currentModel := firstNonEmpty(configuredClaudeModel(s.app.cfg), claudeDefaultModelAlias)
	currentEffort := firstNonEmpty(configuredClaudeEffort(s.app.cfg), "(default)")

	elements := []map[string]any{
		{
			"tag": "markdown",
			"content": "当前 backend: `claude`\n" +
				"当前模型: `" + currentModel + "`\n" +
				"当前推理强度: `" + currentEffort + "`\n\n" +
				"这里提供 Claude 常用别名与当前自定义 model。\n" +
				"需要任意 raw model 时，请直接使用 `/model set <model-id>`。\n" +
				"`/model set default` 会恢复为 `sonnet`。\n" +
				"切换 Claude model / effort 只允许在当前 frontend 空闲时进行；成功后会尝试立即应用到当前会话，并用于后续对话。",
		},
		{"tag": "markdown", "content": "选择 Claude 默认模型"},
	}

	modelOptions := make([]selectStaticOption, 0, len(claudeBuiltinModelOptions)+1)
	for _, item := range claudeModelPickerOptions(s.app.cfg) {
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

	effortValue := configuredClaudeEffort(s.app.cfg)
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

func (s modelConfigService) ensureClaudeRuntimeConfigChangeSafe() error {
	if s.app == nil {
		return fmt.Errorf("app not initialized")
	}
	if reason := strings.TrimSpace(frontendIdleBlockedReason(s.app)); reason != "" {
		return fmt.Errorf("Claude model / effort 只能在当前 frontend 空闲时切换: %s", reason)
	}
	return nil
}

func (s modelConfigService) updateClaudeModelConfig(mutate func(*config.ClaudeConfig)) error {
	if s.app.cfg == nil {
		return fmt.Errorf("nil config")
	}
	if strings.TrimSpace(s.app.cfgPath) == "" {
		return fmt.Errorf("missing config path")
	}
	if err := newModelConfigService(s.app).ensureClaudeRuntimeConfigChangeSafe(); err != nil {
		return err
	}
	s.app.configMu.Lock()
	defer s.app.configMu.Unlock()
	mutate(&s.app.cfg.Claude)
	if err := s.app.cfg.Normalize(filepath.Dir(s.app.cfgPath)); err != nil {
		return err
	}
	if err := config.Save(s.app.cfgPath, s.app.cfg); err != nil {
		return err
	}
	if s.app.claude != nil {
		s.app.claude.UpdateConfig(s.app.cfg.Claude)
	}
	return nil
}

func (s modelConfigService) hotApplyClaudeModelToCurrentSession(sessionKey, model string) (bool, error) {
	if s.app == nil || s.app.claude == nil {
		return false, nil
	}
	sessionKey = normalizeSessionKey(s.app, sessionKey)
	if sessionKey == "" || !sessionBelongsToFrontend(s.app, sessionKey) {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.app.claude.SetModel(ctx, sessionKey, strings.TrimSpace(model))
}

func (s modelConfigService) hotApplyClaudeEffortToCurrentSession(sessionKey, effort string) (bool, error) {
	if s.app == nil || s.app.claude == nil {
		return false, nil
	}
	sessionKey = normalizeSessionKey(s.app, sessionKey)
	if sessionKey == "" || !sessionBelongsToFrontend(s.app, sessionKey) {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.app.claude.SetEffort(ctx, sessionKey, strings.TrimSpace(effort))
}

func (s modelConfigService) completeClaudeModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	model := normalizeClaudeModelValue(modelID)
	if err := newModelConfigService(s.app).updateClaudeModelConfig(func(c *config.ClaudeConfig) {
		c.Model = model
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	toastType := "success"
	toastContent := "已更新 Claude 模型；后续对话会使用新配置"
	if applied, err := newModelConfigService(s.app).hotApplyClaudeModelToCurrentSession(sessionKey, model); err != nil {
		toastType = "warning"
		toastContent = "已更新 Claude 模型；当前会话热更新失败，仅后续对话会使用新配置"
	} else if applied {
		toastContent = "已更新 Claude 模型；当前会话与后续对话会使用新配置"
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: toastType, Content: toastContent},
		Card:  rawCard(newModelConfigService(s.app).renderClaudeModelConfigCard(sessionKey, menuAction)),
	}, nil
}

func (s modelConfigService) completeClaudeEffortSet(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	normalized, err := config.NormalizeClaudeEffort(effort)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	if err := newModelConfigService(s.app).updateClaudeModelConfig(func(c *config.ClaudeConfig) {
		c.Effort = normalized
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	toastType := "success"
	toastContent := "已更新 Claude 推理强度；后续对话会使用新配置"
	if applied, applyErr := newModelConfigService(s.app).hotApplyClaudeEffortToCurrentSession(sessionKey, normalized); applyErr != nil {
		toastType = "warning"
		switch {
		case errors.Is(applyErr, claudecli.ErrEffortDefaultHotApplyUnsupported):
			toastContent = "已更新 Claude 推理强度；当前会话暂不支持热切回默认，仅后续对话会使用新配置"
		default:
			toastContent = "已更新 Claude 推理强度；当前会话热更新失败，仅后续对话会使用新配置"
		}
	} else if applied {
		toastContent = "已更新 Claude 推理强度；当前会话与后续对话会使用新配置"
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: toastType, Content: toastContent},
		Card:  rawCard(newModelConfigService(s.app).renderClaudeModelConfigCard(sessionKey, menuAction)),
	}, nil
}

func (s modelConfigService) commandClaudeModel(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	sessionKey := makeSessionKey(s.app, msg)
	if len(args) > 0 {
		action := commandActionFromMessage(msg, map[string]any{
			"menu_action": "menu.model",
			"session_key": sessionKey,
		})
		switch strings.TrimSpace(args[0]) {
		case "set":
			if len(args) != 2 {
				return fmt.Errorf("usage: %s", modelCommandUsage)
			}
			resp, err := newModelConfigService(s.app).completeClaudeModelSet(action, strings.TrimSpace(args[1]))
			if err != nil {
				return err
			}
			return replyCommandActionResponse(s.app, msg, resp)
		case "effort":
			if len(args) != 2 {
				return fmt.Errorf("usage: %s", modelCommandUsage)
			}
			effort := strings.TrimSpace(args[1])
			if effort == "default" || effort == modelConfigDefaultOptionValue {
				effort = ""
			}
			resp, err := newModelConfigService(s.app).completeClaudeEffortSet(action, effort)
			if err != nil {
				return err
			}
			return replyCommandActionResponse(s.app, msg, resp)
		default:
			return fmt.Errorf("usage: %s", modelCommandUsage)
		}
	}
	card := newModelConfigService(s.app).renderClaudeModelConfigCard(sessionKey, "menu.model")
	_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}

func (s modelConfigService) commandEffort(msg *feishu.InboundMessage, args []string) error {
	switch len(args) {
	case 0:
		return newModelConfigService(s.app).commandModel(msg, nil)
	case 1:
		return newModelConfigService(s.app).commandModel(msg, []string{"effort", strings.TrimSpace(args[0])})
	default:
		return fmt.Errorf("usage: %s", effortCommandUsage)
	}
}
