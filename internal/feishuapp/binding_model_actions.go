package feishuapp

import (
	appmenuutil "feidex/internal/adapter/feishu/menuutil"
	appservicetiercmd "feidex/internal/adapter/feishu/servicetier"
	applicationrouting "feidex/internal/application/routing"
	domainbackend "feidex/internal/domain/backend"
	catalog "feidex/internal/domain/modelconfig"
	"feidex/internal/domain/routing"
	"feidex/internal/textutil"

	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/adapter/feishu/cards"
	appmodelconfig "feidex/internal/adapter/feishu/modelconfig"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s bindingService) renderBindingModelConfigCard(sessionKey string, binding *state.AgentBinding) (map[string]any, error) {
	if binding == nil {
		binding = s.scope.Binding(sessionKey)
	}
	switch s.deps.ConfiguredBackend() {
	case domainbackend.BackendCodex:
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := s.deps.ModelCommands.FetchModelList(ctx)
		if err != nil {
			return nil, err
		}
		return s.renderBindingCodexModelConfigCard(sessionKey, binding, result), nil
	case domainbackend.BackendClaude:
		return s.renderBindingClaudeModelConfigCard(sessionKey, binding), nil
	default:
		body := strings.Join([]string{
			"backend: `" + textutil.FirstNonEmpty(s.deps.ConfiguredBackend(), "unset") + "`",
			unsupportedGroupModelBackendMessage(s.deps.ConfiguredBackend()),
		}, "\n")
		return appmenuutil.PageCard{
			Node: "menu.model", SessionKey: sessionKey, Title: "模型配置", Color: "orange", Body: body,
		}.Render(), nil
	}
}

func (s bindingService) renderBindingCodexModelConfigCard(sessionKey string, binding *state.AgentBinding, result catalog.ModelListResult) map[string]any {
	if binding == nil {
		binding = &state.AgentBinding{}
	}
	modelOverride := strings.TrimSpace(binding.ModelOverride)
	effortOverride := strings.TrimSpace(binding.ReasoningEffortOverride)
	sess := s.deps.State.Session(sessionKey)
	settings, origins := s.deps.ModelSnapshots.DesiredTraced(domainbackend.BackendCodex, sess)
	selectedModel := appmodelconfig.FindModelEntry(result, settings.Model)
	selectedEffort := settings.Effort
	if selectedEffort == "" && selectedModel != nil {
		selectedEffort = strings.TrimSpace(selectedModel.DefaultReasoningEffort)
	}
	if !appmodelconfig.ModelSupportsEffort(selectedModel, selectedEffort) && selectedModel != nil {
		selectedEffort = strings.TrimSpace(selectedModel.DefaultReasoningEffort)
	}
	modelName := "(default)"
	modelDescription := ""
	if selectedModel != nil {
		modelName = textutil.FirstNonEmpty(selectedModel.DisplayName, selectedModel.ID, selectedModel.Model)
		modelDescription = strings.TrimSpace(selectedModel.Description)
	}

	// 来源显示基于统一解析器的层标注；未显式配置时显示实际生效的 Bot 默认值。
	modelSource := groupSettingSourceText(origins.Model, settings.Model, "跟随 Bot 默认 (app-server 默认)")
	effortSource := groupEffortSourceText(origins.Effort, settings.Effort, selectedModel)

	// 辅助模型摘要显示实际生效值；未显式配置时跟随 Bot 默认。
	planModelDisplay := renderAuxModelSummary(binding.PlanModelOverride, settings.PlanModel, modelName)
	reviewModelDisplay := renderAuxModelSummary(binding.ReviewModelOverride, settings.ReviewModel, modelName)
	subagentModelDisplay := renderAuxModelSummary(binding.SubagentModelOverride, settings.SubagentModel, modelName)

	content := "当前模型: `" + modelName + "`\n" +
		"模型来源: " + modelSource + "\n" +
		"当前推理强度: `" + textutil.FirstNonEmpty(selectedEffort, "-") + "`\n" +
		"推理来源: " + effortSource + "\n\n辅助模型摘要:\nplan: " + planModelDisplay + "\nreview: " + reviewModelDisplay + "\nsubagent: " + subagentModelDisplay

	if modelDescription != "" {
		content += "\n\n" + modelDescription
	}
	elements := []map[string]any{
		{"tag": "markdown", "content": content},
		{"tag": "markdown", "content": "选择模型"},
	}

	modelOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if modelOverride == "" {
				return "当前 · 跟随 Bot 默认"
			}
			return "跟随 Bot 默认"
		}(),
		Value: appmodelconfig.DefaultOptionValue,
	}}
	modelInitialOption := appmodelconfig.DefaultOptionValue
	if modelOverride != "" && selectedModel != nil {
		modelInitialOption = selectedModel.ID
	}
	for _, item := range result.Data {
		label := textutil.FirstNonEmpty(item.DisplayName, item.ID, item.Model)
		if selectedModel != nil && item.ID == selectedModel.ID && modelOverride != "" {
			label = "当前 · " + label
		}
		modelOptions = append(modelOptions, cards.SelectStaticOption{Text: label, Value: item.ID})
	}
	elements = append(elements, cards.BuildSelectStaticElement(
		"model_config_select_model",
		"选择模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": "menu.model"},
		modelOptions,
		modelInitialOption,
	))

	elements = append(elements, map[string]any{"tag": "markdown", "content": "选择推理强度"})
	effortOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if effortOverride == "" {
				return "当前 · 跟随模型或 Bot 默认"
			}
			return "跟随模型或 Bot 默认"
		}(),
		Value: appmodelconfig.DefaultOptionValue,
	}}
	effortInitialOption := appmodelconfig.DefaultOptionValue
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
	elements = append(elements, cards.BuildSelectStaticElement(
		"model_config_select_effort",
		"选择推理强度",
		map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": "menu.model"},
		effortOptions,
		effortInitialOption,
	))
	elements = append(elements, appmodelconfig.ModelCardActionRow([]feishu.Button{
		{
			Text:  "配置辅助模型",
			Type:  "default",
			Value: map[string]any{"action": "menu.model_auxiliary", "session_key": sessionKey},
		},
		appmodelconfig.FastConfigButton(sessionKey),
	}))
	return appmenuutil.MarkdownPageCard{
		Node: "menu.model", SessionKey: sessionKey, Title: "模型配置", Color: "blue",
		Elements: elements,
		Tail:     []map[string]any{{"tag": "markdown", "content": s.modelConfigStatus(sessionKey)}},
	}.Render()
}

func (s bindingService) renderBindingClaudeModelConfigCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	cfg := configReadCopy(s.deps.Config, s.deps.ConfigMu)
	if binding == nil {
		binding = &state.AgentBinding{}
	}
	modelOverride := strings.TrimSpace(binding.ModelOverride)
	effortOverride := strings.TrimSpace(binding.ReasoningEffortOverride)
	sess := s.deps.State.Session(sessionKey)
	settings, origins := s.deps.ModelSnapshots.DesiredTraced(domainbackend.BackendClaude, sess)
	currentModel := textutil.FirstNonEmpty(settings.Model, appmodelconfig.ClaudeDefaultModelAlias)
	currentEffort := textutil.FirstNonEmpty(settings.Effort, "(default)")
	modelSource := groupSettingSourceText(origins.Model, settings.Model, "跟随 Bot 默认 (`"+appmodelconfig.ClaudeDefaultModelAlias+"`)")
	effortSource := groupSettingSourceText(origins.Effort, settings.Effort, "跟随 Bot 默认 (default)")

	// 辅助模型摘要显示实际生效值；未显式配置时跟随 Bot 默认。
	smallModelDisplay := renderAuxModelSummary(binding.SmallModelOverride, settings.SmallModel, "Claude 内置 haiku")
	subagentModelDisplay := renderAuxModelSummary(binding.SubagentModelOverride, settings.SubagentModel, currentModel)

	elements := []map[string]any{
		{"tag": "markdown", "content": "当前模型: `" + currentModel + "`\n模型来源: " + modelSource + "\n当前推理强度: `" + currentEffort + "`\n推理来源: " + effortSource + "\n\n辅助模型摘要:\nsmall: " + smallModelDisplay + "\nsubagent: " + subagentModelDisplay + "\n\n需要任意 raw model 时，请直接使用 `/model set <model-id>`。"},
		{"tag": "markdown", "content": "选择模型"},
	}

	// The picker marks the group override, never the Bot default it falls back to.
	modelOptions := append([]cards.SelectStaticOption{{
		Text: func() string {
			if modelOverride == "" {
				return "当前 · 跟随 Bot 默认"
			}
			return "跟随 Bot 默认"
		}(),
		Value: appmodelconfig.DefaultOptionValue,
	}}, appmodelconfig.ClaudeModelSelectOptions(cfg, modelOverride)...)
	modelInitialOption := appmodelconfig.DefaultOptionValue
	if modelOverride != "" {
		modelInitialOption = modelOverride
	}
	elements = append(elements, cards.BuildSelectStaticElement(
		"claude_model_config_select_model",
		"选择模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": "menu.model"},
		modelOptions,
		modelInitialOption,
	))

	elements = append(elements, map[string]any{"tag": "markdown", "content": "选择推理强度"})
	effortOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if effortOverride == "" {
				return "当前 · 跟随 Bot 默认"
			}
			return "跟随 Bot 默认"
		}(),
		Value: appmodelconfig.DefaultOptionValue,
	}}
	effortInitialOption := appmodelconfig.DefaultOptionValue
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
	elements = append(elements, cards.BuildSelectStaticElement(
		"claude_model_config_select_effort",
		"选择推理强度",
		map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": "menu.model"},
		effortOptions,
		effortInitialOption,
	))
	elements = append(elements, appmodelconfig.RenderClaudeModelOptionConfigElements(cfg, sessionKey, "menu.model")...)
	elements = append(elements, appmodelconfig.ModelCardActionRow([]feishu.Button{{
		Text:  "配置辅助模型",
		Type:  "default",
		Value: map[string]any{"action": "menu.model_auxiliary", "session_key": sessionKey},
	}}))
	return appmenuutil.MarkdownPageCard{
		Node: "menu.model", SessionKey: sessionKey, Title: "模型配置", Color: "blue",
		Elements: elements,
		Tail:     []map[string]any{{"tag": "markdown", "content": s.modelConfigStatus(sessionKey)}},
	}.Render()
}

func (s bindingService) renderBindingAuxiliaryModelConfigCard(sessionKey string, binding *state.AgentBinding) (map[string]any, error) {
	cfg := configReadCopy(s.deps.Config, s.deps.ConfigMu)
	if binding == nil {
		binding = s.scope.Binding(sessionKey)
	}
	elements := []map[string]any{}
	switch s.deps.ConfiguredBackend() {
	case domainbackend.BackendClaude:
		small, subagent := "", ""
		if binding != nil {
			small, subagent = binding.SmallModelOverride, binding.SubagentModelOverride
		}
		// Each dropdown marks its own override; the Bot default stays unmarked.
		auxModelOptions := func(current string) []cards.SelectStaticOption {
			return append([]cards.SelectStaticOption{{Text: "跟随 Bot 默认", Value: appmodelconfig.DefaultOptionValue}}, appmodelconfig.ClaudeModelSelectOptions(cfg, current)...)
		}
		elements = append(elements, map[string]any{"tag": "markdown", "content": "**small model（Haiku）**\nClaude 内部执行轻量任务时使用；未设置时跟随 Bot 默认。"})
		elements = append(elements, cards.BuildSelectStaticElement("group_aux_small", "small model（Haiku）", map[string]any{"action": "model.aux_config.select_small_model", "session_key": sessionKey}, auxModelOptions(small), textutil.FirstNonEmpty(small, appmodelconfig.DefaultOptionValue)))
		elements = append(elements, map[string]any{"tag": "markdown", "content": "**subagent model**\nClaude 内部自动创建子 agent 时使用；未设置时跟随 Bot 默认。"})
		elements = append(elements, cards.BuildSelectStaticElement("group_aux_subagent", "subagent model", map[string]any{"action": "model.aux_config.select_subagent_model", "session_key": sessionKey}, auxModelOptions(subagent), textutil.FirstNonEmpty(subagent, appmodelconfig.DefaultOptionValue)))
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := s.deps.ModelCommands.FetchModelList(ctx)
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
		planEntry := appmodelconfig.FindModelEntry(result, textutil.FirstNonEmpty(planModel, appmodelconfig.ConfiguredGlobalModel(cfg)))
		planOptions := []cards.SelectStaticOption{{Text: "跟随 Bot 默认", Value: appmodelconfig.DefaultOptionValue}}
		for _, item := range result.Data {
			planOptions = append(planOptions, cards.SelectStaticOption{Text: textutil.FirstNonEmpty(item.DisplayName, item.ID, item.Model), Value: item.ID})
		}
		elements = append(elements, map[string]any{"tag": "markdown", "content": "**Plan 模型**\n用于 `/plan` 模式；未设置时跟随 Bot 默认。"})
		elements = append(elements, cards.BuildSelectStaticElement("group_aux_plan", "Plan 模型", map[string]any{"action": "model.aux_config.select_plan_model", "session_key": sessionKey}, planOptions, textutil.FirstNonEmpty(planModel, appmodelconfig.DefaultOptionValue)))
		planEffortOptions := []cards.SelectStaticOption{{Text: "跟随 Plan preset", Value: appmodelconfig.DefaultOptionValue}}
		if planEntry != nil {
			for _, item := range planEntry.SupportedReasoningEfforts {
				planEffortOptions = append(planEffortOptions, cards.SelectStaticOption{Text: item.ReasoningEffort, Value: item.ReasoningEffort})
			}
		}
		elements = append(elements, map[string]any{"tag": "markdown", "content": "**Plan 推理强度**\n未设置时跟随 Bot 默认的 Plan preset。"})
		elements = append(elements, cards.BuildSelectStaticElement("group_aux_plan_effort", "Plan 推理强度", map[string]any{"action": "model.aux_config.select_plan_effort", "session_key": sessionKey}, planEffortOptions, textutil.FirstNonEmpty(planEffort, appmodelconfig.DefaultOptionValue)))
		modelOptions := func(current string) []cards.SelectStaticOption {
			options := []cards.SelectStaticOption{{Text: "跟随 Bot 默认", Value: appmodelconfig.DefaultOptionValue}}
			for _, item := range result.Data {
				options = append(options, cards.SelectStaticOption{Text: textutil.FirstNonEmpty(item.DisplayName, item.ID, item.Model), Value: item.ID})
			}
			return options
		}
		elements = append(elements, map[string]any{"tag": "markdown", "content": "**review 模型**\n用于 `/review` 自动发起的代码审查；未设置时跟随 Bot 默认。"})
		elements = append(elements, cards.BuildSelectStaticElement("group_aux_review", "review 模型", map[string]any{"action": "model.aux_config.select_review_model", "session_key": sessionKey}, modelOptions(review), textutil.FirstNonEmpty(review, appmodelconfig.DefaultOptionValue)))
		elements = append(elements, map[string]any{"tag": "markdown", "content": "**subagent 模型**\n用于 Codex 自动创建的子 agent；未设置时跟随 Bot 默认。"})
		elements = append(elements, cards.BuildSelectStaticElement("group_aux_subagent", "subagent 模型", map[string]any{"action": "model.aux_config.select_subagent_model", "session_key": sessionKey}, modelOptions(subagent), textutil.FirstNonEmpty(subagent, appmodelconfig.DefaultOptionValue)))
		subagentEntry := appmodelconfig.FindModelEntry(result, subagent)
		effortOptions := []cards.SelectStaticOption{{Text: "跟随 subagent model 默认", Value: appmodelconfig.DefaultOptionValue}}
		if subagentEntry != nil {
			for _, item := range subagentEntry.SupportedReasoningEfforts {
				effortOptions = append(effortOptions, cards.SelectStaticOption{Text: item.ReasoningEffort, Value: item.ReasoningEffort})
			}
		}
		elements = append(elements, map[string]any{"tag": "markdown", "content": "**subagent 推理强度**\n未设置时跟随 subagent 模型的默认强度。"})
		elements = append(elements, cards.BuildSelectStaticElement("group_aux_subagent_effort", "subagent 推理强度", map[string]any{"action": "model.aux_config.select_subagent_effort", "session_key": sessionKey}, effortOptions, textutil.FirstNonEmpty(subagentEffort, appmodelconfig.DefaultOptionValue)))
	}
	return appmenuutil.MarkdownPageCard{
		Node: "menu.model_auxiliary", SessionKey: sessionKey, Title: "辅助模型配置", Color: "blue",
		Body:     "当前群内覆盖。未设置时跟随 Bot 默认；可随时保存，待对应会话边界生效。",
		Elements: elements,
		Tail:     []map[string]any{{"tag": "markdown", "content": s.modelConfigStatus(sessionKey)}},
	}.Render(), nil
}

func (s bindingService) completeBindingAuxiliaryModelSet(action *feishu.CardAction, sessionKey, role, value string) (*callback.CardActionTriggerResponse, error) {
	if err := ensureSessionModelConfigWritable(s.deps.FrontendQuery, sessionKey); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	value = clearableArg(value)
	msg := commandMessageFromAction(s.scope, action, sessionKey, "/model")
	_, err := s.deps.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	result, err := s.deps.ScopedRoutingConfiguration.Set(applicationrouting.Scope{ChatType: msg.ChatType, ChatID: msg.ChatID}, routing.Setting(role), value)
	updated := result.Binding
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := s.renderBindingAuxiliaryModelConfigCard(sessionKey, updated)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已保存当前群内辅助模型配置；待对应会话边界生效"}}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已保存当前群内辅助模型配置；待对应会话边界生效"}, Card: rawCard(card)}, nil
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
	service := s.deps.ModelCommands
	if err := service.UpdateClaudeModelOptionsConfig(value, add); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	msg := commandMessageFromAction(s.scope, action, sessionKey, "/model")
	binding, err := s.deps.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card := s.renderBindingModelConfigOrErrorCard(sessionKey, binding)
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
	service := s.deps.ModelCommands
	if err := service.UpdateClaudeModelOptionsConfig(value, strings.EqualFold(strings.TrimSpace(args[0]), "add")); err != nil {
		return err
	}
	binding, err := s.deps.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
	if err != nil {
		return err
	}
	card := s.renderBindingModelConfigOrErrorCard(s.deps.MakeSessionKey(msg), binding)
	_, err = s.deps.replyCardWithID(context.Background(), msg.MessageID, card, false)
	return err
}

func (s bindingService) renderBindingFastCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	if binding == nil {
		binding = s.scope.Binding(sessionKey)
	}
	current := bindingServiceTierOverride(binding)
	body := strings.Join([]string{
		"配置当前 Bot 在本群的响应速度。",
		"",
		"当前群内响应速度: " + appservicetiercmd.RenderServiceTierValue(appservicetiercmd.NormalizeServiceTier(current)),
	}, "\n")
	defaultLabel := "跟随默认"
	defaultType := "default"
	if strings.TrimSpace(current) == "" {
		defaultLabel = "当前 · 默认"
		defaultType = "primary"
	}
	fastLabel := "fast"
	fastType := "default"
	if appservicetiercmd.NormalizeServiceTier(current) == appservicetiercmd.ServiceTierFast {
		fastLabel = "当前 · fast"
		fastType = "primary"
	}
	buttons := []feishu.Button{
		{Text: defaultLabel, Type: defaultType, Value: map[string]any{"action": "service_tier.set", "session_key": sessionKey, "service_tier": "default"}},
		{Text: fastLabel, Type: fastType, Value: map[string]any{"action": "service_tier.set", "session_key": sessionKey, "service_tier": appservicetiercmd.ServiceTierFast}},
	}
	return appmenuutil.PageCard{
		Node: "menu.fast", SessionKey: sessionKey, Title: "响应速度", Color: "blue",
		Body: body, Buttons: buttons,
	}.Render()
}

func bindingServiceTierOverride(binding *state.AgentBinding) string {
	if binding == nil {
		return ""
	}
	return strings.TrimSpace(binding.ServiceTierOverride)
}

func (s bindingService) renderBindingModelConfigOrErrorCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	card, err := s.renderBindingModelConfigCard(sessionKey, binding)
	if err == nil {
		return card
	}
	return appmenuutil.PageCard{
		Node: "menu.model", SessionKey: sessionKey, Title: "模型配置", Color: "orange",
		Body: "已保存配置，但暂时无法刷新模型配置：" + err.Error(),
		Buttons: []feishu.Button{
			{Text: submenuCommandLabel("重试模型配置", "/model"), Type: "default", Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey}},
		},
	}.Render()
}

// groupSettingSourceText renders the source annotation for a resolved setting
// on a group configuration card: which tier the effective value came from.
// unsetText is used when no tier supplied a value.
func groupSettingSourceText(origin catalog.Origin, value, unsetText string) string {
	switch origin {
	case catalog.OriginBinding:
		return "当前群内显式配置"
	case catalog.OriginSession:
		return "当前会话配置"
	case catalog.OriginProfile, catalog.OriginGlobal, catalog.OriginDerived:
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return "跟随 Bot 默认 (`" + trimmed + "`)"
		}
	}
	return unsetText
}

// groupEffortSourceText renders the effort source annotation, including the
// model-default fallback that applies when no tier configured an effort.
func groupEffortSourceText(origin catalog.Origin, effort string, selectedModel *catalog.ModelListEntry) string {
	if text := groupSettingSourceText(origin, effort, ""); text != "" {
		return text
	}
	if selectedModel != nil && strings.TrimSpace(selectedModel.DefaultReasoningEffort) != "" {
		return "跟随模型默认 (`" + strings.TrimSpace(selectedModel.DefaultReasoningEffort) + "`)"
	}
	return "跟随模型或 Bot 默认"
}

// renderAuxModelSummary renders one auxiliary-model summary entry: the value
// that is actually in effect, annotated with where it comes from.
func renderAuxModelSummary(override, effective, builtinFallback string) string {
	effective = strings.TrimSpace(effective)
	if effective == "" {
		effective = builtinFallback
	}
	if strings.TrimSpace(override) != "" {
		return "`" + effective + "` (当前群内显式配置)"
	}
	return "`" + effective + "` (跟随 Bot 默认)"
}

func unsupportedGroupModelBackendMessage(backend string) string {
	backend = strings.TrimSpace(backend)
	if backend == "" {
		return "当前 frontend 还没有设置 backend，请先选择。"
	}
	return "不支持的 backend: `" + backend + "`。"
}
