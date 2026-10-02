package modelconfig

import (
	"context"
	catalog "feidex/internal/domain/modelconfig"
	"fmt"
	"strings"
	"sync"
	"time"

	"feidex/internal/adapter/feishu/cards"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/runtime"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// DefaultOptionValue is the sentinel value used for "follow default" selections.
const DefaultOptionValue = "__default__"

// ClaudeDefaultModelAlias is the fallback Claude model alias when none is configured.
const ClaudeDefaultModelAlias = "sonnet"

// ModelCommandUsage is the usage string for the /model command.
const ModelCommandUsage = "/model | /model set <model-id|default> | /model effort <effort|default> | /model plan set <model-id|default> | /model plan effort <effort|default> | /model review set <model-id|default> | /model subagent set <model-id|default> | /model subagent effort <effort|default> | /model small set <model-id|default>"

// EffortCommandUsage is the usage string for the /effort command.
const EffortCommandUsage = "/effort | /effort <effort|default>"

// ---------------------------------------------------------------------------
// Variables
// ---------------------------------------------------------------------------

// ClaudeBuiltinModelOptions lists the built-in Claude model picker choices.
var ClaudeBuiltinModelOptions = []runtime.ClaudeModelOption{
	{Value: "sonnet", Label: "Sonnet (`sonnet`)"},
	{Value: "opus", Label: "Opus (`opus`)"},
	{Value: "fable", Label: "Fable (`fable`)"},
	{Value: "haiku", Label: "Haiku (`haiku`)"},
}

// ---------------------------------------------------------------------------
// Interfaces
// ---------------------------------------------------------------------------

// CodexClient is the minimal interface for calling codex RPC methods.
type CodexClient interface {
	ListModels(context.Context, int) (catalog.ModelListResult, error)
	ListCollaborationModes(context.Context) (catalog.CollaborationModeListResponse, error)
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func derefStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

func actionSessionKey(action *feishu.CardAction) string {
	return actionStringValue(action, "session_key")
}

func actionStringValue(action *feishu.CardAction, key string) string {
	if action == nil {
		return ""
	}
	value, _ := action.ActionValue[key].(string)
	return strings.TrimSpace(value)
}

func actionFormStringValue(action *feishu.CardAction, key string) string {
	if action == nil || len(action.FormValue) == 0 {
		return ""
	}
	raw, ok := action.FormValue[key]
	if !ok {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func actionSelectedStringValue(action *feishu.CardAction, formKeys ...string) string {
	if action == nil {
		return ""
	}
	for _, key := range formKeys {
		if value := actionFormStringValue(action, key); value != "" {
			return value
		}
		if value := actionStringValue(action, key); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(action.Option); value != "" {
		return value
	}
	if value := strings.TrimSpace(action.InputValue); value != "" {
		return value
	}
	for _, value := range action.Options {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// CommandActionFromMessage builds a CardAction from an InboundMessage.
func CommandActionFromMessage(msg *feishu.InboundMessage, actionValue map[string]any) *feishu.CardAction {
	if actionValue == nil {
		actionValue = map[string]any{}
	}
	if msg == nil {
		return &feishu.CardAction{ActionValue: actionValue}
	}
	return &feishu.CardAction{
		ActionValue: actionValue,
		UserID:      strings.TrimSpace(msg.UserID),
		ChatID:      strings.TrimSpace(msg.ChatID),
		MessageID:   strings.TrimSpace(msg.MessageID),
	}
}

// ---------------------------------------------------------------------------
// ModelConfigService
// ---------------------------------------------------------------------------

// ModelConfigService handles model configuration display, selection, and
// persistence for both Codex and Claude backends. Callback function fields
// are injected by the app-layer constructor to avoid importing app/.
type ModelConfigService struct {
	Backend func() string
	// ConfigWriter owns normalization, persistence and publication of config
	// mutations. Read callbacks remain separate for card rendering snapshots.
	ConfigWriter interface {
		UpdateConfig(func(*config.Config) error) error
	}
	// Config access callbacks.
	GetConfig   func() *config.Config
	GetConfigMu func() *sync.RWMutex

	// Feishu client callbacks.
	ReplyText func(ctx context.Context, msgID string, text string, replyInThread bool) error
	ReplyCard func(ctx context.Context, msgID string, card map[string]any, replyInThread bool) (string, error)

	// Claude runtime callbacks.
	UpdateClaudeConfig func(cfg config.ClaudeConfig)
	IsClaudeAvailable  func() bool

	// Codex client callback.
	RequireCodexClient func() (CodexClient, error)

	// Session helper callbacks.
	MakeSessionKey           func(msg *feishu.InboundMessage) string
	NormalizeSessionKey      func(sessionKey string) string
	SessionBelongsToFrontend func(sessionKey string) bool
	ReplyInThreadEnabled     func(chatType string) bool

	// SessionConfig returns a config clone with session- and profile-level
	// overrides merged in for scoped sessions (for example p2p), or nil when
	// the session has no scoped config. Model cards render from it so the
	// values they show match what the runtime actually applies.
	SessionConfig func(sessionKey string) *config.Config

	// Backend configuration delegate callbacks.

	// Menu helper callbacks.
	FormatMenuBody           func(action, body string) string
	MenuBackAction           func(action string) string
	ModelConfigBlockedReason func() string
	ModelConfigStatus        func(sessionKey string) string

	// Card action response callback.
	ReplyCommandActionResponse func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error
}

// configForSession resolves the config a session's model cards render from: the
// session-scoped clone when one applies, otherwise the global config.
func (s ModelConfigService) configForSession(sessionKey string) *config.Config {
	if s.SessionConfig != nil {
		if cfg := s.SessionConfig(sessionKey); cfg != nil {
			return cfg
		}
	}
	return s.configSnapshot()
}

// backAction resolves the action a card's back control returns to.
func (s ModelConfigService) backAction(action string) string {
	if s.MenuBackAction != nil {
		if resolved := strings.TrimSpace(s.MenuBackAction(action)); resolved != "" {
			return resolved
		}
	}
	return "menu.root"
}

// auxModelRef renders one auxiliary-model summary entry: the value in effect,
// annotated with whether it was explicitly configured.
func auxModelRef(configured, fallback string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return "`" + configured + "` (显式配置)"
	}
	return "`" + strings.TrimSpace(fallback) + "` (跟随默认)"
}

// ---------------------------------------------------------------------------
// Codex standalone helpers
// ---------------------------------------------------------------------------

// ConfiguredGlobalModel returns the bot-default Codex model ID, or empty string.
func ConfiguredGlobalModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Codex.Model)
}

// ConfiguredGlobalReasoningEffort returns the bot-default reasoning effort, or empty string.
func ConfiguredGlobalReasoningEffort(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Codex.ReasoningEffort)
}

// ConfiguredPlanModel returns the globally configured plan-mode model ID, or empty string.
func ConfiguredPlanModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Codex.PlanModel)
}

// ConfiguredPlanReasoningEffort returns the globally configured plan-mode reasoning effort, or empty string.
func ConfiguredPlanReasoningEffort(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Codex.PlanReasoningEffort)
}

// DefaultModelEntry returns the default model from the result, or the first entry if none is marked default.
func DefaultModelEntry(result catalog.ModelListResult) *catalog.ModelListEntry {
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

// LookupModelEntry finds a model by ID or Model field; returns nil if not found.
func LookupModelEntry(result catalog.ModelListResult, modelID string) *catalog.ModelListEntry {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return DefaultModelEntry(result)
	}
	for i := range result.Data {
		if result.Data[i].ID == modelID || result.Data[i].Model == modelID {
			return &result.Data[i]
		}
	}
	return nil
}

// FindModelEntry finds a model by ID, falling back to the default entry.
func FindModelEntry(result catalog.ModelListResult, modelID string) *catalog.ModelListEntry {
	if found := LookupModelEntry(result, modelID); found != nil {
		return found
	}
	return DefaultModelEntry(result)
}

// ModelSupportsEffort reports whether the model supports the given reasoning effort.
func ModelSupportsEffort(model *catalog.ModelListEntry, effort string) bool {
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

// EffectiveConfiguredModelAndEffort resolves the effective model and effort from config and model catalog.
func EffectiveConfiguredModelAndEffort(cfg *config.Config, result catalog.ModelListResult) (model *catalog.ModelListEntry, effort string) {
	model = FindModelEntry(result, ConfiguredGlobalModel(cfg))
	effort = ConfiguredGlobalReasoningEffort(cfg)
	if effort == "" && model != nil {
		effort = strings.TrimSpace(model.DefaultReasoningEffort)
	}
	if !ModelSupportsEffort(model, effort) && model != nil {
		effort = strings.TrimSpace(model.DefaultReasoningEffort)
	}
	return model, effort
}

// EffectivePlanConfiguredModelAndEffort resolves the effective plan-mode model and effort.
func EffectivePlanConfiguredModelAndEffort(cfg *config.Config, result catalog.ModelListResult, preset *catalog.CollaborationModeMask) (model *catalog.ModelListEntry, effort string) {
	switch planModel := ConfiguredPlanModel(cfg); {
	case planModel != "":
		model = FindModelEntry(result, planModel)
	default:
		model = FindModelEntry(result, ConfiguredGlobalModel(cfg))
	}
	effort = ConfiguredPlanReasoningEffort(cfg)
	if effort == "" && preset != nil && preset.ReasoningEffort != nil {
		effort = strings.TrimSpace(*preset.ReasoningEffort)
	}
	return model, effort
}

// FindPlanCollaborationModePreset returns the plan collaboration-mode preset.
func FindPlanCollaborationModePreset(resp catalog.CollaborationModeListResponse) (*catalog.CollaborationModeMask, error) {
	for i := range resp.Data {
		mode := strings.TrimSpace(derefStringPtr(resp.Data[i].Mode))
		if mode == "plan" {
			return &resp.Data[i], nil
		}
	}
	return nil, fmt.Errorf("当前 Codex app-server 未提供 `plan` collaboration mode")
}

// ModelCardActionRow builds a card action row element from buttons.
func ModelCardActionRow(buttons []feishu.Button) map[string]any {
	return cards.BuildMarkdownBodyCardActionElement(buttons)
}

// ChunkButtons splits a button slice into rows of the given size.
func ChunkButtons(buttons []feishu.Button, size int) [][]feishu.Button {
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

// ---------------------------------------------------------------------------
// Claude standalone helpers
// ---------------------------------------------------------------------------

// ConfiguredClaudeModel returns the configured Claude model, or empty string.
func ConfiguredClaudeModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Claude.Model)
}

// ConfiguredClaudeEffort returns the configured Claude effort, or empty string.
func ConfiguredClaudeEffort(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Claude.Effort)
}

// ConfiguredClaudeModelOptions returns the configured extra Claude model picker options.
func ConfiguredClaudeModelOptions(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	return NormalizeClaudeModelOptions(cfg.Claude.ModelOptions)
}

// NormalizeClaudeModelValue normalizes a Claude model value, mapping empty/default to the built-in alias.
func NormalizeClaudeModelValue(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "", "default", DefaultOptionValue:
		return ClaudeDefaultModelAlias
	default:
		return value
	}
}

// NormalizeClaudeModelOptions trims, drops empties, and de-duplicates Claude model picker options.
func NormalizeClaudeModelOptions(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// AddClaudeModelOption appends a model picker option if it is not already present.
func AddClaudeModelOption(values []string, model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return NormalizeClaudeModelOptions(values)
	}
	values = append(NormalizeClaudeModelOptions(values), model)
	return NormalizeClaudeModelOptions(values)
}

// RemoveClaudeModelOption removes a model picker option.
func RemoveClaudeModelOption(values []string, model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return NormalizeClaudeModelOptions(values)
	}
	out := make([]string, 0, len(values))
	for _, value := range NormalizeClaudeModelOptions(values) {
		if value == model {
			continue
		}
		out = append(out, value)
	}
	return out
}

// ClaudeModelPickerOptions builds the Claude model candidate list: the built-in
// aliases followed by the configured candidate models. Labels carry no current
// selection marker; each picker marks its own value via ClaudeModelSelectOptions.
func ClaudeModelPickerOptions(cfg *config.Config) []runtime.ClaudeModelOption {
	configuredOptions := ConfiguredClaudeModelOptions(cfg)
	options := make([]runtime.ClaudeModelOption, 0, len(ClaudeBuiltinModelOptions)+len(configuredOptions))
	seen := map[string]struct{}{}
	for _, item := range ClaudeBuiltinModelOptions {
		if _, ok := seen[item.Value]; ok {
			continue
		}
		options = append(options, item)
		seen[item.Value] = struct{}{}
	}
	for _, model := range configuredOptions {
		if _, ok := seen[model]; ok {
			continue
		}
		options = append(options, runtime.ClaudeModelOption{
			Value: model,
			Label: "配置 (`" + model + "`)",
		})
		seen[model] = struct{}{}
	}
	return options
}

// ClaudeModelSelectOptions renders one Claude model picker, marking the entry
// that matches the value that picker is currently set to. An empty current
// marks nothing; a current value outside the candidate list is appended as a
// custom entry so the picker can display it.
func ClaudeModelSelectOptions(cfg *config.Config, current string) []cards.SelectStaticOption {
	current = strings.TrimSpace(current)
	if current == DefaultOptionValue {
		current = ""
	}
	options := make([]cards.SelectStaticOption, 0, len(ClaudeBuiltinModelOptions)+len(ConfiguredClaudeModelOptions(cfg))+1)
	seen := map[string]struct{}{}
	for _, item := range ClaudeModelPickerOptions(cfg) {
		label := item.Label
		if current != "" && item.Value == current {
			label = "当前 · " + label
		}
		options = append(options, cards.SelectStaticOption{Text: label, Value: item.Value})
		seen[item.Value] = struct{}{}
	}
	if current != "" {
		if _, ok := seen[current]; !ok {
			options = append(options, cards.SelectStaticOption{
				Text:  "当前 · 自定义 (`" + current + "`)",
				Value: current,
			})
		}
	}
	return options
}

// ---------------------------------------------------------------------------
// ModelConfigService — Codex model methods
// ---------------------------------------------------------------------------

// FetchModelList fetches the Codex model catalog.
func (s ModelConfigService) FetchModelList(ctx context.Context) (catalog.ModelListResult, error) {
	var result catalog.ModelListResult
	client, err := s.RequireCodexClient()
	if err != nil {
		return result, err
	}
	return client.ListModels(ctx, 100)
}

// FetchPlanCollaborationModePreset fetches the plan collaboration-mode preset.
func (s ModelConfigService) FetchPlanCollaborationModePreset(ctx context.Context) (*catalog.CollaborationModeMask, error) {
	client, err := s.RequireCodexClient()
	if err != nil {
		return nil, err
	}
	result, err := client.ListCollaborationModes(ctx)
	if err != nil {
		return nil, err
	}
	return FindPlanCollaborationModePreset(result)
}

// RenderModelConfigCard renders the Codex model configuration card.
func (s ModelConfigService) RenderModelConfigCard(result catalog.ModelListResult, _ *catalog.CollaborationModeMask, sessionKey, menuAction string) map[string]any {
	menuAction = strings.TrimSpace(menuAction)
	if menuAction == "" {
		menuAction = "menu.model"
	}
	cfg := s.configForSession(sessionKey)
	selectedModel, selectedEffort := EffectiveConfiguredModelAndEffort(cfg, result)
	modelName := "(default)"
	modelDescription := ""
	if selectedModel != nil {
		modelName = firstNonEmpty(selectedModel.DisplayName, selectedModel.ID, selectedModel.Model)
		modelDescription = strings.TrimSpace(selectedModel.Description)
	}
	modelValue := ConfiguredGlobalModel(cfg)
	effortValue := ConfiguredGlobalReasoningEffort(cfg)
	modelSource := "跟随 app-server 默认"
	if modelValue != "" {
		modelSource = "Bot 默认显式配置"
	}
	effortSource := "跟随模型默认"
	if effortValue != "" {
		effortSource = "Bot 默认显式配置"
	}
	// plan, review and subagent models are configured on the auxiliary page.
	// Keep their effective values visible here.
	planValue, reviewValue, subagentValue := "", "", ""
	if cfg != nil {
		planValue = strings.TrimSpace(cfg.Codex.PlanModel)
		reviewValue = strings.TrimSpace(cfg.Codex.ReviewModel)
		subagentValue = strings.TrimSpace(cfg.Codex.SubagentModel)
	}
	elements := []map[string]any{
		{
			"tag": "markdown",
			"content": "当前模型: `" + modelName + "`\n" +
				"模型来源: " + modelSource + "\n" +
				"当前推理强度: `" + firstNonEmpty(selectedEffort, "-") + "`\n" +
				"推理来源: " + effortSource + "\n\n" +
				"辅助模型摘要:\nplan: " + auxModelRef(planValue, modelName) +
				"\nreview: " + auxModelRef(reviewValue, modelName) +
				"\nsubagent: " + auxModelRef(subagentValue, modelName) +
				func() string {
					if modelDescription == "" {
						return ""
					}
					return "\n\n" + modelDescription
				}(),
		},
		{"tag": "markdown", "content": "选择模型"},
	}
	modelOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if modelValue == "" {
				return "当前 · 跟随默认"
			}
			return "跟随默认"
		}(),
		Value: DefaultOptionValue,
	}}
	modelInitialOption := DefaultOptionValue
	if modelValue != "" && selectedModel != nil {
		modelInitialOption = selectedModel.ID
	}
	for _, item := range result.Data {
		label := firstNonEmpty(item.DisplayName, item.ID, item.Model)
		if selectedModel != nil && item.ID == selectedModel.ID && modelValue != "" {
			label = "当前 · " + label
		}
		modelOptions = append(modelOptions, cards.SelectStaticOption{
			Text:  label,
			Value: item.ID,
		})
	}
	elements = append(elements, cards.BuildSelectStaticElement(
		"model_config_select_model",
		"选择模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": menuAction},
		modelOptions,
		modelInitialOption,
	))

	elements = append(elements, map[string]any{"tag": "markdown", "content": "选择推理强度"})
	effortOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if effortValue == "" {
				return "当前 · 跟随默认"
			}
			return "跟随默认"
		}(),
		Value: DefaultOptionValue,
	}}
	effortInitialOption := DefaultOptionValue
	if effortValue != "" {
		effortInitialOption = selectedEffort
	}
	if selectedModel != nil {
		for _, item := range selectedModel.SupportedReasoningEfforts {
			label := item.ReasoningEffort
			if item.ReasoningEffort == selectedEffort && effortValue != "" {
				label = "当前 · " + label
			}
			effortOptions = append(effortOptions, cards.SelectStaticOption{
				Text:  label,
				Value: item.ReasoningEffort,
			})
		}
	}
	elements = append(elements, cards.BuildSelectStaticElement(
		"model_config_select_effort",
		"选择推理强度",
		map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": menuAction},
		effortOptions,
		effortInitialOption,
	))

	elements = append(elements, ModelCardActionRow([]feishu.Button{{
		Text:  "配置辅助模型",
		Type:  "default",
		Value: map[string]any{"action": "menu.model_auxiliary", "session_key": sessionKey, "menu_action": menuAction},
	}}))
	if strings.TrimSpace(sessionKey) != "" {
		elements = append(elements, ModelCardActionRow([]feishu.Button{{
			Text:  feishu.MenuBackButtonText,
			Type:  "default",
			Value: map[string]any{"action": s.backAction(menuAction), "session_key": sessionKey},
		}}))
	}

	card := cards.NewMarkdownBodyCard("模型配置", "blue")
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": s.FormatMenuBody(menuAction, "")})
	for _, elem := range elements {
		cards.AppendMarkdownBodyCardElement(card, elem)
	}
	s.appendApplyStatus(card, sessionKey)
	return card
}

// RenderCodexAuxiliaryModelConfigCard renders the secondary Codex model page.
func (s ModelConfigService) RenderCodexAuxiliaryModelConfigCard(result catalog.ModelListResult, planPreset *catalog.CollaborationModeMask, sessionKey, menuAction string) map[string]any {
	cfg := s.configSnapshot()
	planModel, planEffort := EffectivePlanConfiguredModelAndEffort(cfg, result, planPreset)
	planModelValue := ConfiguredPlanModel(cfg)
	planEffortValue := ConfiguredPlanReasoningEffort(cfg)
	modelOptions := modelPickerOptions(result.Data, planModel, planModelValue)
	planEffortOptions := effortPickerOptions(planModel, planEffort, planEffortValue, planPreset)
	reviewValue := ""
	subagentValue := ""
	subagentEffort := ""
	if cfg != nil {
		reviewValue, subagentValue, subagentEffort = cfg.Codex.ReviewModel, cfg.Codex.SubagentModel, cfg.Codex.SubagentReasoningEffort
	}
	card := cards.NewMarkdownBodyCard("辅助模型配置", "blue")
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**Plan 模型**\n用于 `/plan` 模式；未设置时跟随主模型。"})
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("model_aux_plan_model", "Plan 模型", map[string]any{"action": "model.plan_config.select_model", "session_key": sessionKey, "menu_action": "menu.model_auxiliary"}, modelOptions, firstNonEmpty(planModelValue, DefaultOptionValue)))
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**Plan 推理强度**\n未设置时跟随 Codex 的 Plan preset。"})
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("model_aux_plan_effort", "Plan 推理强度", map[string]any{"action": "model.plan_config.select_effort", "session_key": sessionKey, "menu_action": "menu.model_auxiliary"}, planEffortOptions, firstNonEmpty(planEffortValue, DefaultOptionValue)))
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**review 模型**\n用于 `/review` 自动发起的代码审查。"})
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("model_aux_review_model", "review 模型", map[string]any{"action": "model.aux_config.select_review_model", "session_key": sessionKey, "menu_action": "menu.model_auxiliary"}, modelPickerOptions(result.Data, FindModelEntry(result, reviewValue), reviewValue), firstNonEmpty(reviewValue, DefaultOptionValue)))
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**subagent 模型**\n用于 Codex 自动创建的子 agent；未设置时跟随主模型。"})
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("model_aux_subagent_model", "subagent 模型", map[string]any{"action": "model.aux_config.select_subagent_model", "session_key": sessionKey, "menu_action": "menu.model_auxiliary"}, modelPickerOptions(result.Data, FindModelEntry(result, subagentValue), subagentValue), firstNonEmpty(subagentValue, DefaultOptionValue)))
	selectedSubagent := FindModelEntry(result, subagentValue)
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**subagent 推理强度**\n未设置时跟随 subagent 模型的默认强度。"})
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("model_aux_subagent_effort", "subagent 推理强度", map[string]any{"action": "model.aux_config.select_subagent_effort", "session_key": sessionKey, "menu_action": "menu.model_auxiliary"}, effortPickerOptions(selectedSubagent, subagentEffort, subagentEffort, nil), firstNonEmpty(subagentEffort, DefaultOptionValue)))
	cards.AppendMarkdownBodyCardElement(card, ModelCardActionRow([]feishu.Button{{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.model", "session_key": sessionKey}}}))
	s.appendApplyStatus(card, sessionKey)
	return card
}

func modelPickerOptions(entries []catalog.ModelListEntry, selected *catalog.ModelListEntry, configured string) []cards.SelectStaticOption {
	options := []cards.SelectStaticOption{{Text: "跟随主模型", Value: DefaultOptionValue}}
	for _, item := range entries {
		label := firstNonEmpty(item.DisplayName, item.ID, item.Model)
		if configured != "" && selected != nil && item.ID == selected.ID {
			label = "当前 · " + label
		}
		options = append(options, cards.SelectStaticOption{Text: label, Value: item.ID})
	}
	return options
}

func effortPickerOptions(model *catalog.ModelListEntry, selected, configured string, preset *catalog.CollaborationModeMask) []cards.SelectStaticOption {
	label := "跟随默认"
	if configured == "" && preset != nil && preset.ReasoningEffort != nil && strings.TrimSpace(*preset.ReasoningEffort) != "" {
		label = "跟随 Plan preset"
	}
	options := []cards.SelectStaticOption{{Text: label, Value: DefaultOptionValue}}
	if model != nil {
		for _, item := range model.SupportedReasoningEfforts {
			options = append(options, cards.SelectStaticOption{Text: item.ReasoningEffort, Value: item.ReasoningEffort})
		}
	}
	return options
}

func (s ModelConfigService) CompleteCodexAuxiliaryModelSet(action *feishu.CardAction, role, value string) (*callback.CardActionTriggerResponse, error) {
	value = normalizeClearableValue(value)
	if err := s.UpdateGlobalAuxiliaryConfig(func(c *config.CodexConfig) {
		switch role {
		case "review":
			c.ReviewModel = value
		case "subagent":
			c.SubagentModel = value
		case "subagent_effort":
			c.SubagentReasoningEffort = value
		}
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	resp := &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已保存辅助模型配置；待对应会话边界生效"}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if result, err := s.FetchModelList(ctx); err == nil {
		preset, _ := s.FetchPlanCollaborationModePreset(ctx)
		resp.Card = rawCard(s.RenderCodexAuxiliaryModelConfigCard(result, preset, actionSessionKey(action), "menu.model_auxiliary"))
	}
	return resp, nil
}

// UpdateGlobalModelConfig persists a Codex config mutation.
func (s ModelConfigService) UpdateGlobalModelConfig(mutate func(*config.CodexConfig), result catalog.ModelListResult) error {
	if err := s.ensureModelConfigWritable(); err != nil {
		return err
	}
	if s.ConfigWriter == nil {
		return fmt.Errorf("configuration writer unavailable")
	}
	return s.ConfigWriter.UpdateConfig(func(cfg *config.Config) error {
		mutate(&cfg.Codex)
		cfg.Codex.Model = strings.TrimSpace(cfg.Codex.Model)
		cfg.Codex.ReasoningEffort = strings.TrimSpace(cfg.Codex.ReasoningEffort)
		cfg.Codex.PlanModel = strings.TrimSpace(cfg.Codex.PlanModel)
		cfg.Codex.PlanReasoningEffort = strings.TrimSpace(cfg.Codex.PlanReasoningEffort)
		selectedModel := FindModelEntry(result, cfg.Codex.Model)
		if !ModelSupportsEffort(selectedModel, cfg.Codex.ReasoningEffort) {
			cfg.Codex.ReasoningEffort = ""
		}
		selectedPlanModel, _ := EffectivePlanConfiguredModelAndEffort(cfg, result, nil)
		if !ModelSupportsEffort(selectedPlanModel, cfg.Codex.PlanReasoningEffort) {
			cfg.Codex.PlanReasoningEffort = ""
		}
		return nil
	})
}

func (s ModelConfigService) UpdateGlobalAuxiliaryConfig(mutate func(*config.CodexConfig)) error {
	if err := s.ensureModelConfigWritable(); err != nil {
		return err
	}
	if s.ConfigWriter == nil {
		return fmt.Errorf("configuration writer unavailable")
	}
	return s.ConfigWriter.UpdateConfig(func(cfg *config.Config) error {
		mutate(&cfg.Codex)
		return nil
	})
}

// CompleteCodexPlanModelSet handles the plan-mode model selection card action.
func (s ModelConfigService) CompleteCodexPlanModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := s.FetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	modelID = strings.TrimSpace(modelID)
	if modelID != "" && LookupModelEntry(result, modelID) == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未找到 model: " + modelID}}, nil
	}
	if err := s.UpdateGlobalModelConfig(func(c *config.CodexConfig) {
		c.PlanModel = modelID
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已保存 Plan 模式模型；下一次 Plan 轮次应用"},
		Card:  rawCard(s.RenderModelConfigCard(result, nil, sessionKey, menuAction)),
	}, nil
}

// CompleteCodexPlanReasoningEffortSet handles the plan-mode effort selection card action.
func (s ModelConfigService) CompleteCodexPlanReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := s.FetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	selectedPlanModel, _ := EffectivePlanConfiguredModelAndEffort(s.configSnapshot(), result, nil)
	reasoningEffort = strings.TrimSpace(reasoningEffort)
	if reasoningEffort != "" && !ModelSupportsEffort(selectedPlanModel, reasoningEffort) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "Plan 模式模型不支持这个推理强度"}}, nil
	}
	if err := s.UpdateGlobalModelConfig(func(c *config.CodexConfig) {
		c.PlanReasoningEffort = reasoningEffort
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已保存 Plan 模式推理强度；下一次 Plan 轮次应用"},
		Card:  rawCard(s.RenderModelConfigCard(result, nil, sessionKey, menuAction)),
	}, nil
}

// CommandCodexModel handles the /model command for the Codex backend.
func (s ModelConfigService) CommandCodexModel(msg *feishu.InboundMessage, args []string) error {
	sessionKey := s.MakeSessionKey(msg)
	if len(args) > 0 {
		action := CommandActionFromMessage(msg, map[string]any{
			"menu_action": "menu.model",
			"session_key": sessionKey,
		})
		switch strings.TrimSpace(args[0]) {
		case "set":
			if len(args) != 2 {
				return fmt.Errorf("usage: %s", ModelCommandUsage)
			}
			modelID := strings.TrimSpace(args[1])
			if modelID == "default" || modelID == DefaultOptionValue {
				modelID = ""
			}
			if modelID != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				result, err := s.FetchModelList(ctx)
				if err != nil {
					return err
				}
				if LookupModelEntry(result, modelID) == nil {
					return s.ReplyText(context.Background(), msg.MessageID, "未找到 model: "+modelID, s.ReplyInThreadEnabled(msg.ChatType))
				}
			}
			resp, err := s.CompleteCodexGlobalModelSet(action, modelID)
			if err != nil {
				return err
			}
			return s.ReplyCommandActionResponse(msg, resp)
		case "effort":
			if len(args) != 2 {
				return fmt.Errorf("usage: %s", ModelCommandUsage)
			}
			effort := strings.TrimSpace(args[1])
			if effort == "default" || effort == DefaultOptionValue {
				effort = ""
			}
			resp, err := s.CompleteCodexGlobalReasoningEffortSet(action, effort)
			if err != nil {
				return err
			}
			return s.ReplyCommandActionResponse(msg, resp)
		case "plan":
			switch {
			case len(args) == 1:
			case len(args) == 3 && strings.TrimSpace(args[1]) == "set":
				modelID := strings.TrimSpace(args[2])
				if modelID == "default" || modelID == DefaultOptionValue {
					modelID = ""
				}
				resp, err := s.CompleteCodexPlanModelSet(action, modelID)
				if err != nil {
					return err
				}
				return s.ReplyCommandActionResponse(msg, resp)
			case len(args) == 3 && strings.TrimSpace(args[1]) == "effort":
				effort := strings.TrimSpace(args[2])
				if effort == "default" || effort == DefaultOptionValue {
					effort = ""
				}
				resp, err := s.CompleteCodexPlanReasoningEffortSet(action, effort)
				if err != nil {
					return err
				}
				return s.ReplyCommandActionResponse(msg, resp)
			default:
				return fmt.Errorf("usage: %s", ModelCommandUsage)
			}
		case "review":
			if len(args) != 3 || strings.TrimSpace(args[1]) != "set" {
				return fmt.Errorf("usage: /model review set MODEL|default")
			}
			value := strings.TrimSpace(args[2])
			if value == "default" || value == DefaultOptionValue {
				value = ""
			}
			if err := s.UpdateGlobalAuxiliaryConfig(func(c *config.CodexConfig) { c.ReviewModel = value }); err != nil {
				return err
			}
			return s.ReplyText(context.Background(), msg.MessageID, "已保存 Codex review model；待下次新建或恢复 thread 生效", s.ReplyInThreadEnabled(msg.ChatType))
		case "subagent":
			if len(args) == 3 && strings.TrimSpace(args[1]) == "set" {
				value := strings.TrimSpace(args[2])
				if value == "default" || value == DefaultOptionValue {
					value = ""
				}
				if err := s.UpdateGlobalAuxiliaryConfig(func(c *config.CodexConfig) { c.SubagentModel = value }); err != nil {
					return err
				}
				return s.ReplyText(context.Background(), msg.MessageID, "已保存 Codex subagent model；待下次新建或恢复 thread 生效", s.ReplyInThreadEnabled(msg.ChatType))
			}
			if len(args) == 3 && strings.TrimSpace(args[1]) == "effort" {
				value := strings.TrimSpace(args[2])
				if value == "default" || value == DefaultOptionValue {
					value = ""
				}
				if err := s.UpdateGlobalAuxiliaryConfig(func(c *config.CodexConfig) { c.SubagentReasoningEffort = value }); err != nil {
					return err
				}
				return s.ReplyText(context.Background(), msg.MessageID, "已保存 Codex subagent reasoning effort；待下次新建或恢复 thread 生效", s.ReplyInThreadEnabled(msg.ChatType))
			}
			return fmt.Errorf("usage: /model subagent set MODEL|default | /model subagent effort EFFORT|default")
		default:
			return fmt.Errorf("usage: %s", ModelCommandUsage)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := s.FetchModelList(ctx)
	if err != nil {
		return err
	}
	card := s.RenderModelConfigCard(result, nil, sessionKey, "menu.model")
	_, err = s.ReplyCard(context.Background(), msg.MessageID, card, s.ReplyInThreadEnabled(msg.ChatType))
	return err
}

// ---------------------------------------------------------------------------
// ModelConfigService — Claude model methods
// ---------------------------------------------------------------------------

// RenderClaudeModelConfigCard renders the Claude model configuration card.
func (s ModelConfigService) RenderClaudeModelConfigCard(sessionKey, menuAction string) map[string]any {
	menuAction = strings.TrimSpace(menuAction)
	if menuAction == "" {
		menuAction = "menu.model"
	}
	cfg := s.configForSession(sessionKey)
	currentModel := firstNonEmpty(ConfiguredClaudeModel(cfg), ClaudeDefaultModelAlias)
	currentEffort := firstNonEmpty(ConfiguredClaudeEffort(cfg), "(default)")

	// 辅助模型摘要显示实际生效值。
	smallValue, subagentValue := "", ""
	if cfg != nil {
		smallValue = strings.TrimSpace(cfg.Claude.SmallModel)
		subagentValue = strings.TrimSpace(cfg.Claude.SubagentModel)
	}

	elements := []map[string]any{
		{
			"tag": "markdown",
			"content": "当前 backend: `claude`\n" +
				"当前模型: `" + currentModel + "`\n" +
				"当前推理强度: `" + currentEffort + "`\n\n" +
				"辅助模型摘要:\nsmall: " + auxModelRef(smallValue, "Claude 内置 haiku") + "\nsubagent: " + auxModelRef(subagentValue, currentModel) + "\n\n" +
				"这里提供 Claude 常用别名、已配置候选 model 与当前自定义 model。\n" +
				"需要任意 raw model 时，请直接使用 `/model set <model-id>`。\n" +
				"`/model set default` 会恢复为 `sonnet`。\n" +
				"模型配置可随时保存；本轮不变，排队消息在下一轮启动前应用最新配置。",
		},
		{"tag": "markdown", "content": "选择模型"},
	}

	// The picker marks the configured model, not the fallback alias it defaults to.
	modelOptions := ClaudeModelSelectOptions(cfg, ConfiguredClaudeModel(cfg))
	elements = append(elements, cards.BuildSelectStaticElement(
		"claude_model_config_select_model",
		"选择模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": menuAction},
		modelOptions,
		currentModel,
	))

	effortValue := ConfiguredClaudeEffort(cfg)
	effortOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if effortValue == "" {
				return "当前 · 跟随默认"
			}
			return "跟随默认"
		}(),
		Value: DefaultOptionValue,
	}}
	effortInitialOption := DefaultOptionValue
	if effortValue != "" {
		effortInitialOption = effortValue
	}
	for _, effort := range config.SupportedClaudeEfforts() {
		label := effort
		if effort == effortValue && effortValue != "" {
			label = "当前 · " + label
		}
		effortOptions = append(effortOptions, cards.SelectStaticOption{
			Text:  label,
			Value: effort,
		})
	}
	elements = append(elements,
		map[string]any{"tag": "markdown", "content": "选择推理强度"},
		cards.BuildSelectStaticElement(
			"claude_model_config_select_effort",
			"选择推理强度",
			map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": menuAction},
			effortOptions,
			effortInitialOption,
		),
	)
	elements = append(elements, renderClaudeModelOptionConfigElements(cfg, sessionKey, menuAction)...)
	elements = append(elements, ModelCardActionRow([]feishu.Button{{
		Text:  "配置辅助模型",
		Type:  "default",
		Value: map[string]any{"action": "menu.model_auxiliary", "session_key": sessionKey, "menu_action": menuAction},
	}}))
	if strings.TrimSpace(sessionKey) != "" {
		elements = append(elements, ModelCardActionRow([]feishu.Button{{
			Text:  feishu.MenuBackButtonText,
			Type:  "default",
			Value: map[string]any{"action": s.backAction(menuAction), "session_key": sessionKey},
		}}))
	}

	card := cards.NewMarkdownBodyCard("模型配置", "blue")
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": s.FormatMenuBody(menuAction, "")})
	for _, elem := range elements {
		cards.AppendMarkdownBodyCardElement(card, elem)
	}
	s.appendApplyStatus(card, sessionKey)
	return card
}

// RenderClaudeAuxiliaryModelConfigCard renders Claude's small/subagent page.
func (s ModelConfigService) RenderClaudeAuxiliaryModelConfigCard(sessionKey, menuAction string) map[string]any {
	cfg := s.configSnapshot()
	small, subagent := "", ""
	if cfg != nil {
		small, subagent = cfg.Claude.SmallModel, cfg.Claude.SubagentModel
	}
	// Each dropdown marks its own value: small and subagent are independent picks.
	smallOptions := append([]cards.SelectStaticOption{{Text: "跟随默认", Value: DefaultOptionValue}}, ClaudeModelSelectOptions(cfg, small)...)
	subagentOptions := append([]cards.SelectStaticOption{{Text: "跟随默认", Value: DefaultOptionValue}}, ClaudeModelSelectOptions(cfg, subagent)...)
	card := cards.NewMarkdownBodyCard("Claude 辅助模型配置", "blue")
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": s.FormatMenuBody(menuAction, "下面分别配置 Claude 的 small model 和 subagent model。")})
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**small model（Haiku）**\nClaude 内部执行轻量任务时使用；未设置时使用 Claude 内置 Haiku 默认。"})
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("claude_aux_small_model", "small model（Haiku）", map[string]any{"action": "model.aux_config.select_small_model", "session_key": sessionKey, "menu_action": "menu.model_auxiliary"}, smallOptions, firstNonEmpty(small, DefaultOptionValue)))
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": "**subagent model**\nClaude 内部自动创建子 agent 时使用；未设置时跟随主模型。"})
	cards.AppendMarkdownBodyCardElement(card, cards.BuildSelectStaticElement("claude_aux_subagent_model", "subagent model", map[string]any{"action": "model.aux_config.select_subagent_model", "session_key": sessionKey, "menu_action": "menu.model_auxiliary"}, subagentOptions, firstNonEmpty(subagent, DefaultOptionValue)))
	cards.AppendMarkdownBodyCardElement(card, ModelCardActionRow([]feishu.Button{{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.model", "session_key": sessionKey}}}))
	s.appendApplyStatus(card, sessionKey)
	return card
}

func (s ModelConfigService) CompleteClaudeAuxiliaryModelSet(action *feishu.CardAction, role, value string) (*callback.CardActionTriggerResponse, error) {
	value = normalizeClearableValue(value)
	if err := s.UpdateClaudeAuxiliaryConfig(func(c *config.ClaudeConfig) {
		if role == "small" {
			c.SmallModel = value
		} else {
			c.SubagentModel = value
		}
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已保存 Claude 辅助模型配置；待下一轮安全边界生效"}, Card: rawCard(s.RenderClaudeAuxiliaryModelConfigCard(actionSessionKey(action), "menu.model_auxiliary"))}, nil
}

func normalizeClearableValue(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "default", "inherit", "follow", "clear", "unset", DefaultOptionValue:
		return ""
	default:
		return value
	}
}

func renderClaudeModelOptionConfigElements(cfg *config.Config, sessionKey, menuAction string) []map[string]any {
	configuredOptions := ConfiguredClaudeModelOptions(cfg)
	elements := []map[string]any{
		{"tag": "markdown", "content": "管理候选模型"},
	}
	addRows := cards.BuildMarkdownBodyCardActionElements([]feishu.Button{{
		Text: "添加候选模型",
		Type: "primary",
		Name: "claude_model_option_add_submit",
		Value: map[string]any{
			"action":      "model.config.add_option",
			"session_key": sessionKey,
			"menu_action": menuAction,
		},
	}})
	for _, row := range addRows {
		setFirstButtonFormAction(row, "submit")
	}
	elements = append(elements, map[string]any{
		"tag":                "form",
		"name":               "claude_model_option_add_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements": append([]map[string]any{{
			"tag":         "input",
			"name":        "model_id",
			"required":    true,
			"placeholder": map[string]any{"tag": "plain_text", "content": "输入要加入 /model 下拉框的 model id"},
		}}, addRows...),
	})
	if len(configuredOptions) == 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "当前没有额外候选模型。",
		})
		return elements
	}
	removeOptions := make([]cards.SelectStaticOption, 0, len(configuredOptions))
	for _, model := range configuredOptions {
		removeOptions = append(removeOptions, cards.SelectStaticOption{
			Text:  model,
			Value: model,
		})
	}
	elements = append(elements,
		map[string]any{"tag": "markdown", "content": "移除候选模型"},
		cards.BuildSelectStaticElement(
			"claude_model_option_remove_select",
			"选择后立即移除候选模型",
			map[string]any{
				"action":      "model.config.remove_option",
				"session_key": sessionKey,
				"menu_action": menuAction,
			},
			removeOptions,
			"",
		),
	)
	return elements
}

// RenderClaudeModelOptionConfigElements exposes the shared candidate-model
// controls for backend-specific model cards.
func RenderClaudeModelOptionConfigElements(cfg *config.Config, sessionKey, menuAction string) []map[string]any {
	return renderClaudeModelOptionConfigElements(cfg, sessionKey, menuAction)
}

func setFirstButtonFormAction(row map[string]any, actionType string) {
	columns, _ := row["columns"].([]map[string]any)
	if len(columns) == 0 {
		return
	}
	elements, _ := columns[0]["elements"].([]map[string]any)
	if len(elements) == 0 {
		return
	}
	elements[0]["form_action_type"] = actionType
}

func (s ModelConfigService) ensureModelConfigWritable() error {
	if s.ModelConfigBlockedReason != nil {
		if reason := strings.TrimSpace(s.ModelConfigBlockedReason()); reason != "" {
			return fmt.Errorf("模型配置暂不可保存: %s", reason)
		}
	}
	return nil
}

// UpdateClaudeModelConfig persists desired Claude settings for the next turn boundary.
func (s ModelConfigService) UpdateClaudeModelConfig(mutate func(*config.ClaudeConfig)) error {
	return s.updateClaudeModelConfig(mutate, false)
}

func (s ModelConfigService) UpdateClaudeAuxiliaryConfig(mutate func(*config.ClaudeConfig)) error {
	return s.updateClaudeModelConfig(mutate, true)
}

// UpdateClaudeModelOptionsConfig persists Claude picker option changes. This
// does not affect the active runtime model, so it is allowed while the frontend
// is busy.
func (s ModelConfigService) UpdateClaudeModelOptionsConfig(mutate func(*config.ClaudeConfig)) error {
	if s.ConfigWriter == nil {
		return fmt.Errorf("configuration writer unavailable")
	}
	return s.ConfigWriter.UpdateConfig(func(cfg *config.Config) error {
		mutate(&cfg.Claude)
		return nil
	})
}

func (s ModelConfigService) updateClaudeModelConfig(mutate func(*config.ClaudeConfig), ignoreCurrentMessage bool) error {
	if err := s.ensureModelConfigWritable(); err != nil {
		return err
	}
	if s.ConfigWriter == nil {
		return fmt.Errorf("configuration writer unavailable")
	}
	var next config.ClaudeConfig
	if err := s.ConfigWriter.UpdateConfig(func(cfg *config.Config) error {
		mutate(&cfg.Claude)
		next = cfg.Claude
		return nil
	}); err != nil {
		return err
	}
	if s.IsClaudeAvailable() {
		s.UpdateClaudeConfig(next)
	}
	return nil
}

// CompleteClaudeModelSet handles the Claude model selection card action.
func (s ModelConfigService) CompleteClaudeModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	return s.completeClaudeModelSet(action, modelID, false)
}

func (s ModelConfigService) completeClaudeModelSet(action *feishu.CardAction, modelID string, ignoreCurrentMessage bool) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	model := NormalizeClaudeModelValue(modelID)
	if err := s.updateClaudeModelConfig(func(c *config.ClaudeConfig) {
		c.Model = model
	}, ignoreCurrentMessage); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已保存 Claude 模型；本轮不变，下一轮启动前应用"},
		Card:  rawCard(s.RenderClaudeModelConfigCard(sessionKey, menuAction)),
	}, nil
}

// CompleteClaudeModelOptionAdd handles the Claude picker option add form.
func (s ModelConfigService) CompleteClaudeModelOptionAdd(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	model := actionSelectedStringValue(action, "model_id")
	model = strings.TrimSpace(model)
	if model == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请输入 model id"}}, nil
	}
	if err := s.UpdateClaudeModelOptionsConfig(func(c *config.ClaudeConfig) {
		c.ModelOptions = AddClaudeModelOption(c.ModelOptions, model)
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已添加 Claude 候选模型 `" + model + "`"},
		Card:  rawCard(s.RenderClaudeModelConfigCard(sessionKey, menuAction)),
	}, nil
}

// CompleteClaudeModelOptionRemove handles the Claude picker option remove form.
func (s ModelConfigService) CompleteClaudeModelOptionRemove(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	model := actionSelectedStringValue(action, "model_id", "claude_model_option_remove_select")
	model = strings.TrimSpace(model)
	if model == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请选择要移除的 model id"}}, nil
	}
	if err := s.UpdateClaudeModelOptionsConfig(func(c *config.ClaudeConfig) {
		c.ModelOptions = RemoveClaudeModelOption(c.ModelOptions, model)
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已移除 Claude 候选模型 `" + model + "`"},
		Card:  rawCard(s.RenderClaudeModelConfigCard(sessionKey, menuAction)),
	}, nil
}

// CompleteClaudeEffortSet handles the Claude effort selection card action.
func (s ModelConfigService) CompleteClaudeEffortSet(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
	return s.completeClaudeEffortSet(action, effort, false)
}

func (s ModelConfigService) completeClaudeEffortSet(action *feishu.CardAction, effort string, ignoreCurrentMessage bool) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	normalized, err := config.NormalizeClaudeEffort(effort)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	if err := s.updateClaudeModelConfig(func(c *config.ClaudeConfig) {
		c.Effort = normalized
	}, ignoreCurrentMessage); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已保存 Claude 推理强度；本轮不变，下一轮启动前应用"},
		Card:  rawCard(s.RenderClaudeModelConfigCard(sessionKey, menuAction)),
	}, nil
}

// CommandClaudeModel handles the /model command for the Claude backend.
func (s ModelConfigService) CommandClaudeModel(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	sessionKey := s.MakeSessionKey(msg)
	if len(args) > 0 {
		action := CommandActionFromMessage(msg, map[string]any{
			"menu_action": "menu.model",
			"session_key": sessionKey,
		})
		switch strings.TrimSpace(args[0]) {
		case "set":
			if len(args) != 2 {
				return fmt.Errorf("usage: %s", ModelCommandUsage)
			}
			resp, err := s.completeClaudeModelSet(action, strings.TrimSpace(args[1]), true)
			if err != nil {
				return err
			}
			return s.ReplyCommandActionResponse(msg, resp)
		case "effort":
			if len(args) != 2 {
				return fmt.Errorf("usage: %s", ModelCommandUsage)
			}
			effort := strings.TrimSpace(args[1])
			if effort == "default" || effort == DefaultOptionValue {
				effort = ""
			}
			resp, err := s.completeClaudeEffortSet(action, effort, true)
			if err != nil {
				return err
			}
			return s.ReplyCommandActionResponse(msg, resp)
		case "small":
			if len(args) != 3 || strings.TrimSpace(args[1]) != "set" {
				return fmt.Errorf("usage: /model small set MODEL|default")
			}
			value := strings.TrimSpace(args[2])
			if value == "default" || value == DefaultOptionValue {
				value = ""
			}
			if err := s.UpdateClaudeAuxiliaryConfig(func(c *config.ClaudeConfig) { c.SmallModel = value }); err != nil {
				return err
			}
			return s.ReplyText(context.Background(), msg.MessageID, "已保存 Claude small model；待下一轮安全边界生效", s.ReplyInThreadEnabled(msg.ChatType))
		case "subagent":
			if len(args) != 3 || strings.TrimSpace(args[1]) != "set" {
				return fmt.Errorf("usage: /model subagent set MODEL|default")
			}
			value := strings.TrimSpace(args[2])
			if value == "default" || value == DefaultOptionValue {
				value = ""
			}
			if err := s.UpdateClaudeAuxiliaryConfig(func(c *config.ClaudeConfig) { c.SubagentModel = value }); err != nil {
				return err
			}
			return s.ReplyText(context.Background(), msg.MessageID, "已保存 Claude subagent model；待下一轮安全边界生效", s.ReplyInThreadEnabled(msg.ChatType))
		case "option":
			if len(args) != 3 {
				return fmt.Errorf("usage: %s", ModelCommandUsage)
			}
			switch strings.TrimSpace(args[1]) {
			case "add":
				action.FormValue = map[string]any{"model_id": strings.TrimSpace(args[2])}
				resp, err := s.CompleteClaudeModelOptionAdd(action)
				if err != nil {
					return err
				}
				return s.ReplyCommandActionResponse(msg, resp)
			case "remove", "delete", "rm":
				action.FormValue = map[string]any{"model_id": strings.TrimSpace(args[2])}
				resp, err := s.CompleteClaudeModelOptionRemove(action)
				if err != nil {
					return err
				}
				return s.ReplyCommandActionResponse(msg, resp)
			default:
				return fmt.Errorf("usage: %s", ModelCommandUsage)
			}
		default:
			return fmt.Errorf("usage: %s", ModelCommandUsage)
		}
	}
	card := s.RenderClaudeModelConfigCard(sessionKey, "menu.model")
	_, err := s.ReplyCard(context.Background(), msg.MessageID, card, s.ReplyInThreadEnabled(msg.ChatType))
	return err
}

// CommandEffort handles the /effort command.
func (s ModelConfigService) CommandEffort(msg *feishu.InboundMessage, args []string) error {
	switch len(args) {
	case 0:
		return s.CommandModel(msg, nil)
	case 1:
		return s.CommandModel(msg, []string{"effort", strings.TrimSpace(args[0])})
	default:
		return fmt.Errorf("usage: %s", EffortCommandUsage)
	}
}

func (s ModelConfigService) configSnapshot() *config.Config {
	mu := s.GetConfigMu()
	mu.RLock()
	defer mu.RUnlock()
	return config.Clone(s.GetConfig())
}

func (s ModelConfigService) CommandModel(msg *feishu.InboundMessage, args []string) error {
	if s.Backend != nil && s.Backend() == "claude" {
		return s.CommandClaudeModel(msg, args)
	}
	return s.CommandCodexModel(msg, args)
}

func (s ModelConfigService) CompleteCodexGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := s.FetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if err := s.UpdateGlobalModelConfig(func(c *config.CodexConfig) {
		c.Model = strings.TrimSpace(modelID)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已保存 Bot 默认模型；本轮不变，下一轮启动前应用"},
		Card:  rawCard(s.RenderModelConfigCard(result, nil, sessionKey, menuAction)),
	}, nil
}

func (s ModelConfigService) CompleteCodexGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := actionSessionKey(action)
	menuAction := actionStringValue(action, "menu_action")
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := s.FetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	selectedModel, _ := EffectiveConfiguredModelAndEffort(s.configSnapshot(), result)
	if strings.TrimSpace(reasoningEffort) != "" && !ModelSupportsEffort(selectedModel, reasoningEffort) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模型不支持这个推理强度"}}, nil
	}
	if err := s.UpdateGlobalModelConfig(func(c *config.CodexConfig) {
		c.ReasoningEffort = strings.TrimSpace(reasoningEffort)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已保存 Bot 默认推理强度；本轮不变，下一轮启动前应用"},
		Card:  rawCard(s.RenderModelConfigCard(result, nil, sessionKey, menuAction)),
	}, nil
}
