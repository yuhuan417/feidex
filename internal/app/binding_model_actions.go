package app

import (
	"context"
	"strings"
	"time"

	"feidex/internal/app/cards"
	appmodelconfig "feidex/internal/app/modelconfig"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (s bindingService) renderBindingModelMenuCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	if binding == nil {
		binding = bindingForSessionKey(s.app, sessionKey)
	}
	backend := configuredBackend(s.app)
	lines := []string{
		"配置当前 Bot 在本群的模型相关设置。",
		"",
		"backend: `" + firstNonEmpty(backend, "unset") + "`",
		"当前群内模型: " + renderOptionalBacktick(bindingModelOverride(binding)),
	}
	if backend == backendCodex || backend == backendClaude {
		lines = append(lines, "当前群内推理强度: "+renderOptionalBacktick(bindingReasoningEffortOverride(binding)))
	}
	if backend == backendCodex {
		lines = append(lines, "当前群内响应速度: "+renderOptionalBacktick(bindingServiceTierOverride(binding)))
	}
	buttons := []feishu.Button{
		{Text: submenuCommandLabel("模型配置", "/model"), Type: "default", Value: map[string]any{"action": "menu.model", "session_key": sessionKey}},
	}
	if backend == backendCodex {
		buttons = append(buttons, feishu.Button{Text: submenuCommandLabel("响应速度", "/fast config"), Type: "default", Value: map[string]any{"action": "menu.fast", "session_key": sessionKey}})
	}
	buttons = append(buttons, feishu.Button{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}})
	return s.app.feishu.SimpleStatusCard("模型配置", "blue", menuCardBody("menu.group.model", strings.Join(lines, "\n")), buttons)

}

func (s bindingService) renderBindingModelConfigCard(sessionKey string, binding *state.AgentBinding) (map[string]any, error) {
	if binding == nil {
		binding = bindingForSessionKey(s.app, sessionKey)
	}
	switch configuredBackend(s.app) {
	case backendCodex:
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := newModelConfigService(s.app).fetchModelList(ctx)
		if err != nil {
			return nil, err
		}
		return s.renderBindingCodexModelConfigCard(sessionKey, binding, result), nil
	case backendClaude:
		return s.renderBindingClaudeModelConfigCard(sessionKey, binding), nil
	default:
		body := strings.Join([]string{
			"backend: `" + firstNonEmpty(configuredBackend(s.app), "unset") + "`",
			unsupportedGroupModelBackendMessage(configuredBackend(s.app)),
		}, "\n")
		return s.app.feishu.SimpleStatusCard("模型配置", "orange", menuCardBody("menu.model", body), []feishu.Button{{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey}}}), nil
	}

}

func (s bindingService) renderBindingCodexModelConfigCard(sessionKey string, binding *state.AgentBinding, result codexrpc.ModelListResult) map[string]any {
	if binding == nil {
		binding = &state.AgentBinding{}
	}
	modelOverride := strings.TrimSpace(binding.ModelOverride)
	effortOverride := strings.TrimSpace(binding.ReasoningEffortOverride)
	selectedModel := appmodelconfig.FindModelEntry(result, firstNonEmpty(modelOverride, appmodelconfig.ConfiguredGlobalModel(s.app.cfg)))
	selectedEffort := effortOverride
	if selectedEffort == "" {
		selectedEffort = appmodelconfig.ConfiguredGlobalReasoningEffort(s.app.cfg)
	}
	if selectedEffort == "" && selectedModel != nil {
		selectedEffort = strings.TrimSpace(selectedModel.DefaultReasoningEffort)
	}
	if !appmodelconfig.ModelSupportsEffort(selectedModel, selectedEffort) && selectedModel != nil {
		selectedEffort = strings.TrimSpace(selectedModel.DefaultReasoningEffort)
	}
	modelName := "(default)"
	modelDescription := ""
	if selectedModel != nil {
		modelName = firstNonEmpty(selectedModel.DisplayName, selectedModel.ID, selectedModel.Model)
		modelDescription = strings.TrimSpace(selectedModel.Description)
	}
	modelSource := "跟随 Bot 默认"
	if modelOverride != "" {
		modelSource = "当前群内显式配置"
	}
	effortSource := "跟随模型或 Bot 默认"
	if effortOverride != "" {
		effortSource = "当前群内显式配置"
	}

	card := cards.NewMarkdownBodyCard("模型配置", "blue")
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBody("menu.model", "")})
	content := "当前模型: `" + modelName + "`\n" +
		"模型来源: " + modelSource + "\n" +
		"当前推理强度: `" + firstNonEmpty(selectedEffort, "-") + "`\n" +
		"推理来源: " + effortSource

	if modelDescription != "" {
		content += "\n\n" + modelDescription
	}
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": content})
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "选择当前群内模型"})

	modelOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if modelOverride == "" {
				return "当前 · 跟随 Bot 默认"
			}
			return "跟随 Bot 默认"
		}(),
		Value: modelConfigDefaultOptionValue,
	}}
	modelInitialOption := modelConfigDefaultOptionValue
	if modelOverride != "" && selectedModel != nil {
		modelInitialOption = selectedModel.ID
	}
	for _, item := range result.Data {
		label := firstNonEmpty(item.DisplayName, item.ID, item.Model)
		if selectedModel != nil && item.ID == selectedModel.ID && modelOverride != "" {
			label = "当前 · " + label
		}
		modelOptions = append(modelOptions, cards.SelectStaticOption{Text: label, Value: item.ID})
	}
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement(
		"group_model_config_select_model",
		"选择当前群内模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": "menu.model"},
		modelOptions,
		modelInitialOption,
	))

	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "选择当前群内推理强度"})
	effortOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if effortOverride == "" {
				return "当前 · 跟随模型或 Bot 默认"
			}
			return "跟随模型或 Bot 默认"
		}(),
		Value: modelConfigDefaultOptionValue,
	}}
	effortInitialOption := modelConfigDefaultOptionValue
	if effortOverride != "" {
		effortInitialOption = selectedEffort
	}
	if selectedModel != nil {
		for _, item := range selectedModel.SupportedReasoningEfforts {
			effort := strings.TrimSpace(item.ReasoningEffort)
			if effort == "" {
				continue
			}
			label := effort
			if effort == selectedEffort && effortOverride != "" {
				label = "当前 · " + label
			}
			effortOptions = append(effortOptions, cards.SelectStaticOption{Text: label, Value: effort})
		}
	}
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement(
		"group_model_config_select_effort",
		"选择当前群内推理强度",
		map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": "menu.model"},
		effortOptions,
		effortInitialOption,
	))
	cards.AppendMarkdownBodyCardElement(card, modelCardActionRow([]feishu.Button{{
		Text:  "返回上一级",
		Type:  "default",
		Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey},
	}}))
	return card

}

func (s bindingService) renderBindingClaudeModelConfigCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	if binding == nil {
		binding = &state.AgentBinding{}
	}
	modelOverride := strings.TrimSpace(binding.ModelOverride)
	effortOverride := strings.TrimSpace(binding.ReasoningEffortOverride)
	currentModel := firstNonEmpty(modelOverride, appmodelconfig.ConfiguredClaudeModel(s.app.cfg), appmodelconfig.ClaudeDefaultModelAlias)
	currentEffort := firstNonEmpty(effortOverride, appmodelconfig.ConfiguredClaudeEffort(s.app.cfg), "(default)")
	modelSource := "跟随 Bot 默认"
	if modelOverride != "" {
		modelSource = "当前群内显式配置"
	}
	effortSource := "跟随 Bot 默认"
	if effortOverride != "" {
		effortSource = "当前群内显式配置"
	}

	card := cards.NewMarkdownBodyCard("模型配置", "blue")
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBody("menu.model", "")})
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "当前模型: `" + currentModel + "`\n模型来源: " + modelSource + "\n当前推理强度: `" + currentEffort + "`\n推理来源: " + effortSource + "\n\n需要任意 raw model 时，请直接使用 `/model set <model-id>`。"})
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "选择当前群内模型"})

	modelOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if modelOverride == "" {
				return "当前 · 跟随 Bot 默认"
			}
			return "跟随 Bot 默认"
		}(),
		Value: modelConfigDefaultOptionValue,
	}}
	modelInitialOption := modelConfigDefaultOptionValue
	if modelOverride != "" {
		modelInitialOption = modelOverride
	}
	seen := map[string]struct{}{modelConfigDefaultOptionValue: {}}
	for _, item := range appmodelconfig.ClaudeModelPickerOptions(s.app.cfg) {
		value := strings.TrimSpace(item.Value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		label := strings.TrimSpace(item.Label)
		if label == "" {
			label = value
		}
		if value == modelOverride && modelOverride != "" {
			label = "当前 · " + label
		}
		modelOptions = append(modelOptions, cards.SelectStaticOption{Text: label, Value: value})
	}
	if modelOverride != "" {
		if _, ok := seen[modelOverride]; !ok {
			modelOptions = append(modelOptions, cards.SelectStaticOption{Text: "当前 · 自定义 (`" + modelOverride + "`)", Value: modelOverride})
		}
	}
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement(
		"group_claude_model_config_select_model",
		"选择当前群内模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": "menu.model"},
		modelOptions,
		modelInitialOption,
	))

	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "选择当前群内推理强度"})
	effortOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if effortOverride == "" {
				return "当前 · 跟随 Bot 默认"
			}
			return "跟随 Bot 默认"
		}(),
		Value: modelConfigDefaultOptionValue,
	}}
	effortInitialOption := modelConfigDefaultOptionValue
	if effortOverride != "" {
		effortInitialOption = effortOverride
	}
	for _, effort := range config.SupportedClaudeEfforts() {
		label := effort
		if effort == effortOverride && effortOverride != "" {
			label = "当前 · " + label
		}
		effortOptions = append(effortOptions, cards.SelectStaticOption{Text: label, Value: effort})
	}
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement(
		"group_claude_model_config_select_effort",
		"选择当前群内推理强度",
		map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": "menu.model"},
		effortOptions,
		effortInitialOption,
	))
	cards.AppendMarkdownBodyCardElement(card, modelCardActionRow([]feishu.Button{{
		Text:  "返回上一级",
		Type:  "default",
		Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey},
	}}))
	return card

}

func (s bindingService) renderBindingFastCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	if binding == nil {
		binding = bindingForSessionKey(s.app, sessionKey)
	}
	current := bindingServiceTierOverride(binding)
	body := strings.Join([]string{
		"配置当前 Bot 在本群的响应速度。",
		"",
		"当前群内响应速度: " + renderServiceTierValue(normalizeServiceTier(current)),
	}, "\n")
	defaultLabel := "跟随默认"
	defaultType := "default"
	if strings.TrimSpace(current) == "" {
		defaultLabel = "当前 · " + defaultLabel
		defaultType = "primary"
	}
	fastLabel := "fast"
	fastType := "default"
	if normalizeServiceTier(current) == serviceTierFast {
		fastLabel = "当前 · fast"
		fastType = "primary"
	}
	buttons := []feishu.Button{
		{Text: defaultLabel, Type: defaultType, Value: map[string]any{"action": "service_tier.set", "session_key": sessionKey, "service_tier": "default"}},
		{Text: fastLabel, Type: fastType, Value: map[string]any{"action": "service_tier.set", "session_key": sessionKey, "service_tier": serviceTierFast}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey}},
	}
	return s.app.feishu.SimpleStatusCard("响应速度", "blue", menuCardBody("menu.fast", body), buttons)

}

func bindingServiceTierOverride(binding *state.AgentBinding) string {
	if binding == nil {
		return ""
	}
	return strings.TrimSpace(binding.ServiceTierOverride)

}

func (s bindingService) renderBindingModelConfigOrMenuCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	card, err := s.renderBindingModelConfigCard(sessionKey, binding)
	if err == nil {
		return card
	}
	return s.renderBindingModelMenuCard(sessionKey, binding)
}

func unsupportedGroupModelBackendMessage(backend string) string {
	backend = strings.TrimSpace(backend)
	if backend == "" {
		return "当前 frontend 还没有设置 backend，请先选择。"
	}
	return "不支持的 backend: `" + backend + "`。"
}
