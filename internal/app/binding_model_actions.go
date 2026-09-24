package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/app/cards"
	appmodelconfig "feidex/internal/app/modelconfig"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
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
	buttons = append(buttons, feishu.Button{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}})
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
		return s.app.feishu.SimpleStatusCard("模型配置", "orange", menuCardBody("menu.model", body), []feishu.Button{{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey}}}), nil
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
		"推理来源: " + effortSource + "\n\n辅助模型摘要:\nplan: `" + firstNonEmpty(binding.PlanModelOverride, "跟随 Bot 默认") + "`\nreview: `" + firstNonEmpty(binding.ReviewModelOverride, "跟随 Bot 默认") + "`\nsubagent: `" + firstNonEmpty(binding.SubagentModelOverride, "跟随 Bot 默认") + "`"

	if modelDescription != "" {
		content += "\n\n" + modelDescription
	}
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": content})
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "选择模型"})

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
		"model_config_select_model",
		"选择模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": "menu.model"},
		modelOptions,
		modelInitialOption,
	))

	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "选择推理强度"})
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
		"model_config_select_effort",
		"选择推理强度",
		map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": "menu.model"},
		effortOptions,
		effortInitialOption,
	))
	planModel := effectiveCodexPlanModel(s.app, s.app.State().Session(sessionKey))
	planEffort := effectiveCodexPlanReasoningEffort(s.app, s.app.State().Session(sessionKey))
	cards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": "Plan 模式模型: `" + firstNonEmpty(planModel, "(default)") + "`\nPlan 推理强度: `" + firstNonEmpty(planEffort, "-") + "`",
	})
	cards.AppendMarkdownBodyCardElement(card, modelCardActionRow([]feishu.Button{{
		Text:  "配置辅助模型",
		Type:  "default",
		Value: map[string]any{"action": "menu.model_auxiliary", "session_key": sessionKey},
	}}))
	cards.AppendMarkdownBodyCardElement(card, modelCardActionRow([]feishu.Button{{
		Text:  feishu.MenuBackButtonText,
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
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "当前模型: `" + currentModel + "`\n模型来源: " + modelSource + "\n当前推理强度: `" + currentEffort + "`\n推理来源: " + effortSource + "\n\n辅助模型摘要:\nsmall: `" + firstNonEmpty(binding.SmallModelOverride, "跟随 Bot 默认") + "`\nsubagent: `" + firstNonEmpty(binding.SubagentModelOverride, "跟随 Bot 默认") + "`\n\n需要任意 raw model 时，请直接使用 `/model set <model-id>`。"})
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "选择模型"})

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
		"claude_model_config_select_model",
		"选择模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": "menu.model"},
		modelOptions,
		modelInitialOption,
	))

	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "选择推理强度"})
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
		"claude_model_config_select_effort",
		"选择推理强度",
		map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": "menu.model"},
		effortOptions,
		effortInitialOption,
	))
	for _, element := range appmodelconfig.RenderClaudeModelOptionConfigElements(s.app.cfg, sessionKey, "menu.model") {
		cards.AppendMarkdownBodyCardElement(card, element)
	}
	cards.AppendMarkdownBodyCardElement(card, modelCardActionRow([]feishu.Button{{
		Text:  "配置辅助模型",
		Type:  "default",
		Value: map[string]any{"action": "menu.model_auxiliary", "session_key": sessionKey},
	}}))
	cards.AppendMarkdownBodyCardElement(card, modelCardActionRow([]feishu.Button{{
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey},
	}}))
	return card

}

func (s bindingService) renderBindingAuxiliaryModelConfigCard(sessionKey string, binding *state.AgentBinding) (map[string]any, error) {
	if binding == nil {
		binding = bindingForSessionKey(s.app, sessionKey)
	}
	card := cards.NewMarkdownBodyCard("辅助模型配置", "blue")
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBody("menu.model_auxiliary", "当前群内覆盖。未设置时跟随 Bot 默认；修改仅在当前会话空闲时生效。")})
	switch configuredBackend(s.app) {
	case backendClaude:
		options := []cards.SelectStaticOption{{Text: "跟随 Bot 默认", Value: modelConfigDefaultOptionValue}}
		for _, item := range appmodelconfig.ClaudeModelPickerOptions(s.app.cfg) {
			options = append(options, cards.SelectStaticOption{Text: item.Label, Value: item.Value})
		}
		small, subagent := "", ""
		if binding != nil {
			small, subagent = binding.SmallModelOverride, binding.SubagentModelOverride
		}
		cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**small model（Haiku）**\nClaude 内部执行轻量任务时使用；未设置时跟随 Bot 默认。"})
		cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("group_aux_small", "small model（Haiku）", map[string]any{"action": "model.aux_config.select_small_model", "session_key": sessionKey}, options, firstNonEmpty(small, modelConfigDefaultOptionValue)))
		cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**subagent model**\nClaude 内部自动创建子 agent 时使用；未设置时跟随 Bot 默认。"})
		cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("group_aux_subagent", "subagent model", map[string]any{"action": "model.aux_config.select_subagent_model", "session_key": sessionKey}, options, firstNonEmpty(subagent, modelConfigDefaultOptionValue)))
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := newModelConfigService(s.app).fetchModelList(ctx)
		if err != nil {
			return nil, err
		}
		planModel := ""
		planEffort := ""
		review, subagent, subagentEffort := "", "", ""
		if binding != nil {
			planModel, planEffort = binding.PlanModelOverride, binding.PlanReasoningEffortOverride
			review, subagent, subagentEffort = binding.ReviewModelOverride, binding.SubagentModelOverride, binding.SubagentReasoningEffortOverride
		}
		planEntry := appmodelconfig.FindModelEntry(result, firstNonEmpty(planModel, appmodelconfig.ConfiguredGlobalModel(s.app.cfg)))
		planOptions := []cards.SelectStaticOption{{Text: "跟随 Bot 默认", Value: modelConfigDefaultOptionValue}}
		for _, item := range result.Data {
			planOptions = append(planOptions, cards.SelectStaticOption{Text: firstNonEmpty(item.DisplayName, item.ID, item.Model), Value: item.ID})
		}
		cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**Plan 模型**\n用于 `/plan` 模式；未设置时跟随 Bot 默认。"})
		cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("group_aux_plan", "Plan 模型", map[string]any{"action": "model.aux_config.select_plan_model", "session_key": sessionKey}, planOptions, firstNonEmpty(planModel, modelConfigDefaultOptionValue)))
		planEffortOptions := []cards.SelectStaticOption{{Text: "跟随 Plan preset", Value: modelConfigDefaultOptionValue}}
		if planEntry != nil {
			for _, item := range planEntry.SupportedReasoningEfforts {
				planEffortOptions = append(planEffortOptions, cards.SelectStaticOption{Text: item.ReasoningEffort, Value: item.ReasoningEffort})
			}
		}
		cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**Plan 推理强度**\n未设置时跟随 Bot 默认的 Plan preset。"})
		cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("group_aux_plan_effort", "Plan 推理强度", map[string]any{"action": "model.aux_config.select_plan_effort", "session_key": sessionKey}, planEffortOptions, firstNonEmpty(planEffort, modelConfigDefaultOptionValue)))
		modelOptions := func(current string) []cards.SelectStaticOption {
			options := []cards.SelectStaticOption{{Text: "跟随 Bot 默认", Value: modelConfigDefaultOptionValue}}
			for _, item := range result.Data {
				options = append(options, cards.SelectStaticOption{Text: firstNonEmpty(item.DisplayName, item.ID, item.Model), Value: item.ID})
			}
			return options
		}
		cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**review 模型**\n用于 `/review` 自动发起的代码审查；未设置时跟随 Bot 默认。"})
		cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("group_aux_review", "review 模型", map[string]any{"action": "model.aux_config.select_review_model", "session_key": sessionKey}, modelOptions(review), firstNonEmpty(review, modelConfigDefaultOptionValue)))
		cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**subagent 模型**\n用于 Codex 自动创建的子 agent；未设置时跟随 Bot 默认。"})
		cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("group_aux_subagent", "subagent 模型", map[string]any{"action": "model.aux_config.select_subagent_model", "session_key": sessionKey}, modelOptions(subagent), firstNonEmpty(subagent, modelConfigDefaultOptionValue)))
		subagentEntry := appmodelconfig.FindModelEntry(result, subagent)
		effortOptions := []cards.SelectStaticOption{{Text: "跟随 subagent model 默认", Value: modelConfigDefaultOptionValue}}
		if subagentEntry != nil {
			for _, item := range subagentEntry.SupportedReasoningEfforts {
				effortOptions = append(effortOptions, cards.SelectStaticOption{Text: item.ReasoningEffort, Value: item.ReasoningEffort})
			}
		}
		cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**subagent 推理强度**\n未设置时跟随 subagent 模型的默认强度。"})
		cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("group_aux_subagent_effort", "subagent 推理强度", map[string]any{"action": "model.aux_config.select_subagent_effort", "session_key": sessionKey}, effortOptions, firstNonEmpty(subagentEffort, modelConfigDefaultOptionValue)))
	}
	cards.AppendMarkdownBodyCardElement(card, modelCardActionRow([]feishu.Button{{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.model", "session_key": sessionKey}}}))
	return card, nil
}

func (s bindingService) completeBindingAuxiliaryModelSet(action *feishu.CardAction, sessionKey, role, value string) (*callback.CardActionTriggerResponse, error) {
	if err := ensureSessionModelConfigIdle(s.app, sessionKey); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	value = clearableArg(value)
	msg := commandMessageFromAction(s.app, action, sessionKey, "/model")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	updated, err := s.updateBinding(binding, func(current *state.AgentBinding) {
		switch role {
		case "plan":
			current.PlanModelOverride = value
		case "plan_effort":
			current.PlanReasoningEffortOverride = value
		case "review":
			current.ReviewModelOverride = value
		case "subagent":
			current.SubagentModelOverride = value
		case "subagent_effort":
			current.SubagentReasoningEffortOverride = value
		case "small":
			current.SmallModelOverride = value
		}
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if configuredBackend(s.app) == backendClaude && s.app.claude != nil {
		if err := s.app.claude.ResetSession(sessionKey); err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
	}
	card, err := s.renderBindingAuxiliaryModelConfigCard(sessionKey, updated)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已更新当前群内辅助模型配置"}}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已更新当前群内辅助模型配置"}, Card: rawCard(card)}, nil
}

func (s bindingService) completeClaudeModelOption(action *feishu.CardAction, sessionKey string, add bool) (*callback.CardActionTriggerResponse, error) {
	value := ""
	if action != nil && action.FormValue != nil {
		if raw, ok := action.FormValue["model_id"]; ok {
			value = strings.TrimSpace(fmt.Sprint(raw))
		}
	}
	if value == "" {
		value = strings.TrimSpace(action.Option)
	}
	if value == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请输入或选择 model id"}}, nil
	}
	service := newModelConfigService(s.app)
	if err := service.inner.UpdateClaudeModelOptionsConfig(func(c *config.ClaudeConfig) {
		if add {
			c.ModelOptions = appmodelconfig.AddClaudeModelOption(c.ModelOptions, value)
		} else {
			c.ModelOptions = appmodelconfig.RemoveClaudeModelOption(c.ModelOptions, value)
		}
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	binding, err := s.ensureBindingForMessage(commandMessageFromAction(s.app, action, sessionKey, "/model"))
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card := s.renderBindingModelConfigOrMenuCard(sessionKey, binding)
	verb := "移除"
	if add {
		verb = "添加"
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已" + verb + " Claude 候选模型 `" + value + "`"}, Card: rawCard(card)}, nil
}

func (s bindingService) commandClaudeModelOption(msg *feishu.InboundMessage, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: /model option add|remove MODEL_ID")
	}
	value := strings.TrimSpace(args[1])
	if value == "" {
		return fmt.Errorf("model id must not be empty")
	}
	if !strings.EqualFold(strings.TrimSpace(args[0]), "add") && !strings.EqualFold(strings.TrimSpace(args[0]), "remove") && !strings.EqualFold(strings.TrimSpace(args[0]), "delete") && !strings.EqualFold(strings.TrimSpace(args[0]), "rm") {
		return fmt.Errorf("usage: /model option add|remove MODEL_ID")
	}
	service := newModelConfigService(s.app)
	if err := service.inner.UpdateClaudeModelOptionsConfig(func(c *config.ClaudeConfig) {
		if strings.EqualFold(strings.TrimSpace(args[0]), "add") {
			c.ModelOptions = appmodelconfig.AddClaudeModelOption(c.ModelOptions, value)
		} else {
			c.ModelOptions = appmodelconfig.RemoveClaudeModelOption(c.ModelOptions, value)
		}
	}); err != nil {
		return err
	}
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return err
	}
	card := s.renderBindingModelConfigOrMenuCard(makeSessionKey(s.app, msg), binding)
	_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
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
		defaultLabel = "当前 · 默认"
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
		{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey}},
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
