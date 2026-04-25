package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

type modelConfigService struct {
	app *App
}
func newModelConfigService(app *App) modelConfigService {
	return modelConfigService{app: app}
}

const modelConfigDefaultOptionValue = "__default__"
const modelCommandUsage = "/model | /model set <model-id|default> | /model effort <effort|default>"

func configuredGlobalModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Codex.Model)
}

func configuredGlobalReasoningEffort(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Codex.ReasoningEffort)
}

func defaultModelEntry(result codexrpc.ModelListResult) *codexrpc.ModelListEntry {
	for i := range result.Data {
		if result.Data[i].IsDefault {
			return &result.Data[i]
		}
	}
	if len(result.Data) == 0 {
		return nil
	}
	return &result.Data[0]
}

func lookupModelEntry(result codexrpc.ModelListResult, modelID string) *codexrpc.ModelListEntry {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return defaultModelEntry(result)
	}
	for i := range result.Data {
		if result.Data[i].ID == modelID || result.Data[i].Model == modelID {
			return &result.Data[i]
		}
	}
	return nil
}

func findModelEntry(result codexrpc.ModelListResult, modelID string) *codexrpc.ModelListEntry {
	if found := lookupModelEntry(result, modelID); found != nil {
		return found
	}
	return defaultModelEntry(result)
}

func modelSupportsEffort(model *codexrpc.ModelListEntry, effort string) bool {
	effort = strings.TrimSpace(effort)
	if model == nil || effort == "" {
		return true
	}
	for _, item := range model.SupportedReasoningEfforts {
		if strings.TrimSpace(item.ReasoningEffort) == effort {
			return true
		}
	}
	return false
}

func effectiveConfiguredModelAndEffort(cfg *config.Config, result codexrpc.ModelListResult) (model *codexrpc.ModelListEntry, effort string) {
	model = findModelEntry(result, configuredGlobalModel(cfg))
	effort = configuredGlobalReasoningEffort(cfg)
	if effort == "" && model != nil {
		effort = strings.TrimSpace(model.DefaultReasoningEffort)
	}
	if !modelSupportsEffort(model, effort) && model != nil {
		effort = strings.TrimSpace(model.DefaultReasoningEffort)
	}
	return model, effort
}

func (s modelConfigService) fetchModelList(ctx context.Context) (codexrpc.ModelListResult, error) {
	var result codexrpc.ModelListResult
	client, err := requireCodexClient(s.app)
	if err != nil {
		return result, err
	}
	if err := client.Call(ctx, "model/list", map[string]any{"limit": 100, "includeHidden": false}, &result); err != nil {
		return result, err
	}
	return result, nil
}

func modelCardActionRow(buttons []feishu.Button) map[string]any {
	return buildMarkdownBodyCardActionElement(buttons)
}

func chunkButtons(buttons []feishu.Button, size int) [][]feishu.Button {
	if len(buttons) == 0 {
		return nil
	}
	if size <= 0 {
		size = len(buttons)
	}
	rows := make([][]feishu.Button, 0, (len(buttons)+size-1)/size)
	for len(buttons) > 0 {
		n := size
		if len(buttons) < n {
			n = len(buttons)
		}
		rows = append(rows, append([]feishu.Button(nil), buttons[:n]...))
		buttons = buttons[n:]
	}
	return rows
}

func (s modelConfigService) renderModelConfigCard(result codexrpc.ModelListResult, sessionKey, menuAction string) map[string]any {
	menuAction = strings.TrimSpace(menuAction)
	if menuAction == "" {
		menuAction = "menu.model"
	}
	selectedModel, selectedEffort := effectiveConfiguredModelAndEffort(s.app.cfg, result)
	modelName := "(default)"
	modelDescription := ""
	if selectedModel != nil {
		modelName = firstNonEmpty(selectedModel.DisplayName, selectedModel.ID, selectedModel.Model)
		modelDescription = strings.TrimSpace(selectedModel.Description)
	}
	modelValue := configuredGlobalModel(s.app.cfg)
	effortValue := configuredGlobalReasoningEffort(s.app.cfg)
	modelSource := "跟随 app-server 默认"
	if modelValue != "" {
		modelSource = "全局显式配置"
	}
	effortSource := "跟随模型默认"
	if effortValue != "" {
		effortSource = "全局显式配置"
	}

	elements := []map[string]any{
		{
			"tag": "markdown",
			"content": "当前模型: `" + modelName + "`\n" +
				"模型来源: " + modelSource + "\n" +
				"当前推理强度: `" + firstNonEmpty(selectedEffort, "-") + "`\n" +
				"推理来源: " + effortSource +
				func() string {
					if modelDescription == "" {
						return ""
					}
					return "\n\n" + modelDescription
				}(),
		},
		{"tag": "markdown", "content": "选择全局模型"},
	}
	modelOptions := []selectStaticOption{{
		Text: func() string {
			if modelValue == "" {
				return "当前 · 跟随默认"
			}
			return "跟随默认"
		}(),
		Value: modelConfigDefaultOptionValue,
	}}
	modelInitialOption := modelConfigDefaultOptionValue
	if modelValue != "" && selectedModel != nil {
		modelInitialOption = selectedModel.ID
	}
	for _, item := range result.Data {
		label := firstNonEmpty(item.DisplayName, item.ID, item.Model)
		if selectedModel != nil && item.ID == selectedModel.ID && modelValue != "" {
			label = "当前 · " + label
		}
		modelOptions = append(modelOptions, selectStaticOption{
			Text:  label,
			Value: item.ID,
		})
	}
	elements = append(elements, buildSelectStaticElement(
		"model_config_select_model",
		"选择全局模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": menuAction},
		modelOptions,
		modelInitialOption,
	))

	elements = append(elements, map[string]any{"tag": "markdown", "content": "选择全局推理强度"})
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
		effortInitialOption = selectedEffort
	}
	if selectedModel != nil {
		for _, item := range selectedModel.SupportedReasoningEfforts {
			label := item.ReasoningEffort
			if item.ReasoningEffort == selectedEffort && effortValue != "" {
				label = "当前 · " + label
			}
			effortOptions = append(effortOptions, selectStaticOption{
				Text:  label,
				Value: item.ReasoningEffort,
			})
		}
	}
	elements = append(elements, buildSelectStaticElement(
		"model_config_select_effort",
		"选择全局推理强度",
		map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": menuAction},
		effortOptions,
		effortInitialOption,
	))
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

func (s modelConfigService) updateGlobalModelConfig(mutate func(*config.CodexConfig), result codexrpc.ModelListResult) error {
	if s.app.cfg == nil {
		return fmt.Errorf("nil config")
	}
	s.app.configMu.Lock()
	defer s.app.configMu.Unlock()
	mutate(&s.app.cfg.Codex)
	s.app.cfg.Codex.Model = strings.TrimSpace(s.app.cfg.Codex.Model)
	s.app.cfg.Codex.ReasoningEffort = strings.TrimSpace(s.app.cfg.Codex.ReasoningEffort)
	selectedModel := findModelEntry(result, s.app.cfg.Codex.Model)
	if !modelSupportsEffort(selectedModel, s.app.cfg.Codex.ReasoningEffort) {
		s.app.cfg.Codex.ReasoningEffort = ""
	}
	if err := s.app.cfg.Normalize(filepath.Dir(s.app.cfgPath)); err != nil {
		return err
	}
	return config.Save(s.app.cfgPath, s.app.cfg)
}

func (s modelConfigService) commandModel(msg *feishu.InboundMessage, args []string) error {
	return newBackendConfigurationService(s.app).handleBackendModelCommand(msg, args)
}

func (s modelConfigService) commandCodexModel(msg *feishu.InboundMessage, args []string) error {
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
			modelID := strings.TrimSpace(args[1])
			if modelID == "default" || modelID == modelConfigDefaultOptionValue {
				modelID = ""
			}
			if modelID != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				result, err := newModelConfigService(s.app).fetchModelList(ctx)
				if err != nil {
					return err
				}
				if lookupModelEntry(result, modelID) == nil {
					return s.app.feishu.ReplyText(context.Background(), msg.MessageID, "未找到 model: "+modelID, replyInThreadEnabled(s.app, msg.ChatType))
				}
			}
			resp, err := newBackendConfigurationService(s.app).completeGlobalModelSet(action, modelID)
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
			resp, err := newBackendConfigurationService(s.app).completeGlobalReasoningEffortSet(action, effort)
			if err != nil {
				return err
			}
			return replyCommandActionResponse(s.app, msg, resp)
		default:
			return fmt.Errorf("usage: %s", modelCommandUsage)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := newModelConfigService(s.app).fetchModelList(ctx)
	if err != nil {
		return err
	}
	card := newModelConfigService(s.app).renderModelConfigCard(result, sessionKey, "menu.model")
	_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}
