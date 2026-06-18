package modelconfig

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"feidex/internal/app/cards"
	"feidex/internal/app/runtime"
	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"

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
const ModelCommandUsage = "/model | /model set <model-id|default> | /model effort <effort|default> | /model option add <model-id> | /model option remove <model-id> | /model plan | /model plan set <model-id|default> | /model plan effort <effort|default>"

// EffortCommandUsage is the usage string for the /effort command.
const EffortCommandUsage = "/effort | /effort <effort|default>"

// ---------------------------------------------------------------------------
// Variables
// ---------------------------------------------------------------------------

// ClaudeBuiltinModelOptions lists the built-in Claude model picker choices.
var ClaudeBuiltinModelOptions = []runtime.ClaudeModelOption{
	{Value: "sonnet", Label: "Sonnet (`sonnet`)"},
	{Value: "opus", Label: "Opus (`opus`)"},
	{Value: "haiku", Label: "Haiku (`haiku`)"},
}

// ---------------------------------------------------------------------------
// Interfaces
// ---------------------------------------------------------------------------

// CodexClient is the minimal interface for calling codex RPC methods.
type CodexClient interface {
	Call(ctx context.Context, method string, params any, result any) error
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
	// Config access callbacks.
	GetConfig   func() *config.Config
	GetCfgPath  func() string
	GetConfigMu func() *sync.RWMutex

	// Feishu client callbacks.
	ReplyText func(ctx context.Context, msgID string, text string, replyInThread bool) error
	ReplyCard func(ctx context.Context, msgID string, card map[string]any, replyInThread bool) (string, error)

	// Claude runtime callbacks.
	UpdateClaudeConfig func(cfg config.ClaudeConfig)
	ClaudeSetModel     func(ctx context.Context, sessionKey, model string) (bool, error)
	ClaudeSetEffort    func(ctx context.Context, sessionKey, effort string) (bool, error)
	IsClaudeAvailable  func() bool

	// Codex client callback.
	RequireCodexClient func() (CodexClient, error)

	// Session helper callbacks.
	MakeSessionKey           func(msg *feishu.InboundMessage) string
	NormalizeSessionKey      func(sessionKey string) string
	SessionBelongsToFrontend func(sessionKey string) bool
	ReplyInThreadEnabled     func(chatType string) bool

	// Backend configuration delegate callbacks.
	CompleteGlobalModelSet           func(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error)
	CompleteGlobalReasoningEffortSet func(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error)
	HandleBackendModelCommand        func(msg *feishu.InboundMessage, args []string) error

	// Menu helper callbacks.
	FormatMenuBody                                  func(action, body string) string
	FrontendIdleBlockedReason                       func() string
	FrontendIdleBlockedReasonIgnoringCurrentMessage func() string

	// Card action response callback.
	ReplyCommandActionResponse func(msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error
}

// ---------------------------------------------------------------------------
// Codex standalone helpers
// ---------------------------------------------------------------------------

// ConfiguredGlobalModel returns the globally configured codex model ID, or empty string.
func ConfiguredGlobalModel(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Codex.Model)
}

// ConfiguredGlobalReasoningEffort returns the globally configured reasoning effort, or empty string.
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
func DefaultModelEntry(result codexrpc.ModelListResult) *codexrpc.ModelListEntry {
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
func LookupModelEntry(result codexrpc.ModelListResult, modelID string) *codexrpc.ModelListEntry {
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
func FindModelEntry(result codexrpc.ModelListResult, modelID string) *codexrpc.ModelListEntry {
	if found := LookupModelEntry(result, modelID); found != nil {
		return found
	}
	return DefaultModelEntry(result)
}

// ModelSupportsEffort reports whether the model supports the given reasoning effort.
func ModelSupportsEffort(model *codexrpc.ModelListEntry, effort string) bool {
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
func EffectiveConfiguredModelAndEffort(cfg *config.Config, result codexrpc.ModelListResult) (model *codexrpc.ModelListEntry, effort string) {
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
func EffectivePlanConfiguredModelAndEffort(cfg *config.Config, result codexrpc.ModelListResult, preset *codexrpc.CollaborationModeMask) (model *codexrpc.ModelListEntry, effort string) {
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
func FindPlanCollaborationModePreset(resp codexrpc.CollaborationModeListResponse) (*codexrpc.CollaborationModeMask, error) {
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

// ClaudeModelPickerOptions builds the list of Claude model picker options with the current selection marked.
func ClaudeModelPickerOptions(cfg *config.Config) []runtime.ClaudeModelOption {
	configuredOptions := ConfiguredClaudeModelOptions(cfg)
	options := make([]runtime.ClaudeModelOption, 0, len(ClaudeBuiltinModelOptions)+len(configuredOptions)+1)
	seen := map[string]struct{}{}
	current := ConfiguredClaudeModel(cfg)
	for _, item := range ClaudeBuiltinModelOptions {
		label := item.Label
		if item.Value == current {
			label = "当前 · " + label
		}
		options = append(options, runtime.ClaudeModelOption{
			Value: item.Value,
			Label: label,
		})
		seen[item.Value] = struct{}{}
	}
	for _, model := range configuredOptions {
		if _, ok := seen[model]; ok {
			continue
		}
		label := "配置 · `" + model + "`"
		if model == current {
			label = "当前 · 配置 (`" + model + "`)"
		}
		options = append(options, runtime.ClaudeModelOption{
			Value: model,
			Label: label,
		})
		seen[model] = struct{}{}
	}
	if current != "" {
		if _, ok := seen[current]; !ok {
			options = append(options, runtime.ClaudeModelOption{
				Value: current,
				Label: "当前 · 自定义 (`" + current + "`)",
			})
		}
	}
	return options
}

// ---------------------------------------------------------------------------
// ModelConfigService — Codex model methods
// ---------------------------------------------------------------------------

// FetchModelList fetches the Codex model catalog.
func (s ModelConfigService) FetchModelList(ctx context.Context) (codexrpc.ModelListResult, error) {
	var result codexrpc.ModelListResult
	client, err := s.RequireCodexClient()
	if err != nil {
		return result, err
	}
	if err := client.Call(ctx, "model/list", map[string]any{"limit": 100, "includeHidden": false}, &result); err != nil {
		return result, err
	}
	return result, nil
}

// FetchPlanCollaborationModePreset fetches the plan collaboration-mode preset.
func (s ModelConfigService) FetchPlanCollaborationModePreset(ctx context.Context) (*codexrpc.CollaborationModeMask, error) {
	client, err := s.RequireCodexClient()
	if err != nil {
		return nil, err
	}
	var result codexrpc.CollaborationModeListResponse
	if err := client.Call(ctx, "collaborationMode/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return FindPlanCollaborationModePreset(result)
}

// RenderModelConfigCard renders the Codex model configuration card.
func (s ModelConfigService) RenderModelConfigCard(result codexrpc.ModelListResult, planPreset *codexrpc.CollaborationModeMask, sessionKey, menuAction string) map[string]any {
	menuAction = strings.TrimSpace(menuAction)
	if menuAction == "" {
		menuAction = "menu.model"
	}
	cfg := s.GetConfig()
	selectedModel, selectedEffort := EffectiveConfiguredModelAndEffort(cfg, result)
	selectedPlanModel, selectedPlanEffort := EffectivePlanConfiguredModelAndEffort(cfg, result, planPreset)
	modelName := "(default)"
	modelDescription := ""
	if selectedModel != nil {
		modelName = firstNonEmpty(selectedModel.DisplayName, selectedModel.ID, selectedModel.Model)
		modelDescription = strings.TrimSpace(selectedModel.Description)
	}
	modelValue := ConfiguredGlobalModel(cfg)
	effortValue := ConfiguredGlobalReasoningEffort(cfg)
	planModelValue := ConfiguredPlanModel(cfg)
	planEffortValue := ConfiguredPlanReasoningEffort(cfg)
	modelSource := "跟随 app-server 默认"
	if modelValue != "" {
		modelSource = "全局显式配置"
	}
	effortSource := "跟随模型默认"
	if effortValue != "" {
		effortSource = "全局显式配置"
	}
	planModelSource := "跟随 default mode"
	if planModelValue != "" {
		planModelSource = "Plan 显式配置"
	}
	planEffortSource := "未设置"
	switch {
	case planEffortValue != "":
		planEffortSource = "Plan 显式配置"
	case planPreset != nil && planPreset.ReasoningEffort != nil && strings.TrimSpace(*planPreset.ReasoningEffort) != "":
		planEffortSource = "跟随 plan preset"
	}
	planModelName := "(default)"
	if selectedPlanModel != nil {
		planModelName = firstNonEmpty(selectedPlanModel.DisplayName, selectedPlanModel.ID, selectedPlanModel.Model)
	}
	planPresetNotice := "Plan preset: 未提供 `reasoning_effort`，留空时不会额外发送。"
	switch {
	case cfg != nil && !cfg.Codex.ExperimentalAPI:
		planPresetNotice = "Plan 模式需要 `[codex].experimental_api = true`。"
	case planPreset != nil:
		planPresetNotice = "Plan preset: 已从 app-server 读取，留空时跟随 preset。"
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
		"选择全局模型",
		map[string]any{"action": "model.config.select_model", "session_key": sessionKey, "menu_action": menuAction},
		modelOptions,
		modelInitialOption,
	))

	elements = append(elements, map[string]any{"tag": "markdown", "content": "选择全局推理强度"})
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
		"选择全局推理强度",
		map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": menuAction},
		effortOptions,
		effortInitialOption,
	))

	elements = append(elements,
		map[string]any{
			"tag": "markdown",
			"content": "Plan 模式模型: `" + planModelName + "`\n" +
				"模型来源: " + planModelSource + "\n" +
				"Plan 推理强度: `" + firstNonEmpty(selectedPlanEffort, "-") + "`\n" +
				"推理来源: " + planEffortSource + "\n\n" +
				planPresetNotice,
		},
		map[string]any{"tag": "markdown", "content": "选择 Plan 模式模型"},
	)

	planModelOptions := []cards.SelectStaticOption{{
		Text: func() string {
			if planModelValue == "" {
				return "当前 · 跟随 default mode"
			}
			return "跟随 default mode"
		}(),
		Value: DefaultOptionValue,
	}}
	planModelInitialOption := DefaultOptionValue
	if planModelValue != "" && selectedPlanModel != nil {
		planModelInitialOption = selectedPlanModel.ID
	}
	for _, item := range result.Data {
		label := firstNonEmpty(item.DisplayName, item.ID, item.Model)
		if selectedPlanModel != nil && item.ID == selectedPlanModel.ID && planModelValue != "" {
			label = "当前 · " + label
		}
		planModelOptions = append(planModelOptions, cards.SelectStaticOption{
			Text:  label,
			Value: item.ID,
		})
	}
	elements = append(elements, cards.BuildSelectStaticElement(
		"model_plan_config_select_model",
		"选择 Plan 模式模型",
		map[string]any{"action": "model.plan_config.select_model", "session_key": sessionKey, "menu_action": menuAction},
		planModelOptions,
		planModelInitialOption,
	))

	elements = append(elements, map[string]any{"tag": "markdown", "content": "选择 Plan 模式推理强度"})
	planEffortOptions := []cards.SelectStaticOption{{
		Text: func() string {
			switch {
			case planEffortValue == "" && planEffortSource == "跟随 plan preset":
				return "当前 · 跟随 plan preset"
			case planEffortValue == "":
				return "当前 · 留空"
			default:
				if planPreset != nil && planPreset.ReasoningEffort != nil && strings.TrimSpace(*planPreset.ReasoningEffort) != "" {
					return "跟随 plan preset"
				}
				return "清除显式配置"
			}
		}(),
		Value: DefaultOptionValue,
	}}
	planEffortInitialOption := DefaultOptionValue
	if planEffortValue != "" {
		planEffortInitialOption = selectedPlanEffort
	}
	if selectedPlanModel != nil {
		for _, item := range selectedPlanModel.SupportedReasoningEfforts {
			label := item.ReasoningEffort
			if item.ReasoningEffort == selectedPlanEffort && planEffortValue != "" {
				label = "当前 · " + label
			}
			planEffortOptions = append(planEffortOptions, cards.SelectStaticOption{
				Text:  label,
				Value: item.ReasoningEffort,
			})
		}
	}
	elements = append(elements, cards.BuildSelectStaticElement(
		"model_plan_config_select_effort",
		"选择 Plan 模式推理强度",
		map[string]any{"action": "model.plan_config.select_effort", "session_key": sessionKey, "menu_action": menuAction},
		planEffortOptions,
		planEffortInitialOption,
	))
	if strings.TrimSpace(sessionKey) != "" {
		elements = append(elements, ModelCardActionRow([]feishu.Button{{
			Text:  "返回上一级",
			Type:  "default",
			Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey},
		}}))
	}

	card := cards.NewMarkdownBodyCard("模型配置", "blue")
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": s.FormatMenuBody(menuAction, "")})
	for _, elem := range elements {
		cards.AppendMarkdownBodyCardElement(card, elem)
	}
	return card
}

// UpdateGlobalModelConfig persists a Codex config mutation.
func (s ModelConfigService) UpdateGlobalModelConfig(mutate func(*config.CodexConfig), result codexrpc.ModelListResult) error {
	cfg := s.GetConfig()
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	mu := s.GetConfigMu()
	mu.Lock()
	defer mu.Unlock()
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
	if err := cfg.Normalize(filepath.Dir(s.GetCfgPath())); err != nil {
		return err
	}
	return config.Save(s.GetCfgPath(), cfg)
}

func (s ModelConfigService) fetchPlanPresetForRender(ctx context.Context) *codexrpc.CollaborationModeMask {
	cfg := s.GetConfig()
	if cfg == nil || !cfg.Codex.ExperimentalAPI {
		return nil
	}
	preset, err := s.FetchPlanCollaborationModePreset(ctx)
	if err != nil {
		return nil
	}
	return preset
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
		Toast: &callback.Toast{Type: "success", Content: "已更新 Plan 模式模型"},
		Card:  rawCard(s.RenderModelConfigCard(result, s.fetchPlanPresetForRender(ctx), sessionKey, menuAction)),
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
	selectedPlanModel, _ := EffectivePlanConfiguredModelAndEffort(s.GetConfig(), result, nil)
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
		Toast: &callback.Toast{Type: "success", Content: "已更新 Plan 模式推理强度"},
		Card:  rawCard(s.RenderModelConfigCard(result, s.fetchPlanPresetForRender(ctx), sessionKey, menuAction)),
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
			resp, err := s.CompleteGlobalModelSet(action, modelID)
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
			resp, err := s.CompleteGlobalReasoningEffortSet(action, effort)
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
	planPreset := s.fetchPlanPresetForRender(ctx)
	card := s.RenderModelConfigCard(result, planPreset, sessionKey, "menu.model")
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
	cfg := s.GetConfig()
	currentModel := firstNonEmpty(ConfiguredClaudeModel(cfg), ClaudeDefaultModelAlias)
	currentEffort := firstNonEmpty(ConfiguredClaudeEffort(cfg), "(default)")

	elements := []map[string]any{
		{
			"tag": "markdown",
			"content": "当前 backend: `claude`\n" +
				"当前模型: `" + currentModel + "`\n" +
				"当前推理强度: `" + currentEffort + "`\n\n" +
				"这里提供 Claude 常用别名、已配置候选 model 与当前自定义 model。\n" +
				"需要任意 raw model 时，请直接使用 `/model set <model-id>`。\n" +
				"`/model set default` 会恢复为 `sonnet`。\n" +
				"切换 Claude model / effort 只允许在当前 frontend 空闲时进行；成功后会尝试立即应用到当前会话，并用于后续对话。",
		},
		{"tag": "markdown", "content": "选择 Claude 默认模型"},
	}

	modelOptions := make([]cards.SelectStaticOption, 0, len(ClaudeBuiltinModelOptions)+1)
	for _, item := range ClaudeModelPickerOptions(cfg) {
		modelOptions = append(modelOptions, cards.SelectStaticOption{
			Text:  item.Label,
			Value: item.Value,
		})
	}
	elements = append(elements, cards.BuildSelectStaticElement(
		"claude_model_config_select_model",
		"选择 Claude 默认模型",
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
		map[string]any{"tag": "markdown", "content": "选择 Claude 推理强度"},
		cards.BuildSelectStaticElement(
			"claude_model_config_select_effort",
			"选择 Claude 推理强度",
			map[string]any{"action": "model.config.select_effort", "session_key": sessionKey, "menu_action": menuAction},
			effortOptions,
			effortInitialOption,
		),
	)
	elements = append(elements, renderClaudeModelOptionConfigElements(cfg, sessionKey, menuAction)...)
	if strings.TrimSpace(sessionKey) != "" {
		elements = append(elements, ModelCardActionRow([]feishu.Button{{
			Text:  "返回上一级",
			Type:  "default",
			Value: map[string]any{"action": "menu.group.model", "session_key": sessionKey},
		}}))
	}

	card := cards.NewMarkdownBodyCard("模型配置", "blue")
	cards.AppendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": s.FormatMenuBody(menuAction, "")})
	for _, elem := range elements {
		cards.AppendMarkdownBodyCardElement(card, elem)
	}
	return card
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

// EnsureClaudeRuntimeConfigChangeSafe checks that the frontend is idle before
// allowing a Claude model/effort configuration change.
func (s ModelConfigService) EnsureClaudeRuntimeConfigChangeSafe() error {
	return s.ensureClaudeRuntimeConfigChangeSafe(false)
}

func (s ModelConfigService) ensureClaudeRuntimeConfigChangeSafe(ignoreCurrentMessage bool) error {
	blockedReason := s.FrontendIdleBlockedReason
	if ignoreCurrentMessage && s.FrontendIdleBlockedReasonIgnoringCurrentMessage != nil {
		blockedReason = s.FrontendIdleBlockedReasonIgnoringCurrentMessage
	}
	if blockedReason != nil {
		if reason := strings.TrimSpace(blockedReason()); reason != "" {
			return fmt.Errorf("Claude model / effort 只能在当前 frontend 空闲时切换: %s", reason)
		}
	}
	return nil
}

// UpdateClaudeModelConfig persists a Claude config mutation and hot-reloads.
func (s ModelConfigService) UpdateClaudeModelConfig(mutate func(*config.ClaudeConfig)) error {
	return s.updateClaudeModelConfig(mutate, false)
}

// UpdateClaudeModelOptionsConfig persists Claude picker option changes. This
// does not affect the active runtime model, so it is allowed while the frontend
// is busy.
func (s ModelConfigService) UpdateClaudeModelOptionsConfig(mutate func(*config.ClaudeConfig)) error {
	cfg := s.GetConfig()
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	cfgPath := s.GetCfgPath()
	if strings.TrimSpace(cfgPath) == "" {
		return fmt.Errorf("missing config path")
	}
	mu := s.GetConfigMu()
	mu.Lock()
	defer mu.Unlock()
	mutate(&cfg.Claude)
	if err := cfg.Normalize(filepath.Dir(cfgPath)); err != nil {
		return err
	}
	return config.Save(cfgPath, cfg)
}

func (s ModelConfigService) updateClaudeModelConfig(mutate func(*config.ClaudeConfig), ignoreCurrentMessage bool) error {
	cfg := s.GetConfig()
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	cfgPath := s.GetCfgPath()
	if strings.TrimSpace(cfgPath) == "" {
		return fmt.Errorf("missing config path")
	}
	if err := s.ensureClaudeRuntimeConfigChangeSafe(ignoreCurrentMessage); err != nil {
		return err
	}
	mu := s.GetConfigMu()
	mu.Lock()
	defer mu.Unlock()
	mutate(&cfg.Claude)
	if err := cfg.Normalize(filepath.Dir(cfgPath)); err != nil {
		return err
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	if s.IsClaudeAvailable() {
		s.UpdateClaudeConfig(cfg.Claude)
	}
	return nil
}

// HotApplyClaudeModelToCurrentSession attempts to hot-apply a model change to
// the active Claude session.
func (s ModelConfigService) HotApplyClaudeModelToCurrentSession(sessionKey, model string) (bool, error) {
	if !s.IsClaudeAvailable() {
		return false, nil
	}
	sessionKey = s.NormalizeSessionKey(sessionKey)
	if sessionKey == "" || !s.SessionBelongsToFrontend(sessionKey) {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.ClaudeSetModel(ctx, sessionKey, strings.TrimSpace(model))
}

// HotApplyClaudeEffortToCurrentSession attempts to hot-apply an effort change
// to the active Claude session.
func (s ModelConfigService) HotApplyClaudeEffortToCurrentSession(sessionKey, effort string) (bool, error) {
	if !s.IsClaudeAvailable() {
		return false, nil
	}
	sessionKey = s.NormalizeSessionKey(sessionKey)
	if sessionKey == "" || !s.SessionBelongsToFrontend(sessionKey) {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.ClaudeSetEffort(ctx, sessionKey, strings.TrimSpace(effort))
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
	toastType := "success"
	toastContent := "已更新 Claude 模型；后续对话会使用新配置"
	if applied, err := s.HotApplyClaudeModelToCurrentSession(sessionKey, model); err != nil {
		toastType = "warning"
		toastContent = "已更新 Claude 模型；当前会话热更新失败，仅后续对话会使用新配置"
	} else if applied {
		toastContent = "已更新 Claude 模型；当前会话与后续对话会使用新配置"
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: toastType, Content: toastContent},
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
	toastType := "success"
	toastContent := "已更新 Claude 推理强度；后续对话会使用新配置"
	if applied, applyErr := s.HotApplyClaudeEffortToCurrentSession(sessionKey, normalized); applyErr != nil {
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
		return s.HandleBackendModelCommand(msg, nil)
	case 1:
		return s.HandleBackendModelCommand(msg, []string{"effort", strings.TrimSpace(args[0])})
	default:
		return fmt.Errorf("usage: %s", EffortCommandUsage)
	}
}
