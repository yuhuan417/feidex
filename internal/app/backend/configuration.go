package backend

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/app/appcore"
	apputil "feidex/internal/app/apputil"
	"feidex/internal/app/cardactions"
	appdebugview "feidex/internal/app/debugview"
	appmodelconfig "feidex/internal/app/modelconfig"
	appquietmode "feidex/internal/app/quietmode"
	appruntime "feidex/internal/app/runtime"
	appthreadview "feidex/internal/app/threadview"
	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/buildinfo"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	// ClaudeWorkspaceCommandUsage is the usage string for /workspace in Claude mode.
	ClaudeWorkspaceCommandUsage = "/workspace | /workspace list | /workspace new | /workspace clone GIT_URL [ID] [--parent DIR] | /workspace use ID | /workspace delete [ID] | /workspace permissions [MODE|inherit]"
)

// ConfigurationService handles backend-specific configuration display and
// model/workspace configuration card rendering.
type ConfigurationService struct {
	App  App
	deps ConfigurationDeps
}

type ConfigurationFormattingDeps struct {
	FormatMenuBody func(action, body string) string
}

type ConfigurationCommandDeps struct {
	HandleCodexModelCommand          func(msg *feishu.InboundMessage, args []string) error
	HandleClaudeModelCommand         func(msg *feishu.InboundMessage, args []string) error
	HandleWorkspacePermissionCommand func(msg *feishu.InboundMessage, args []string, sessionKey string) error
}

type ConfigurationClaudeDeps struct {
	CompleteModelSet          func(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error)
	CompleteModelOptionAdd    func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error)
	CompleteModelOptionRemove func(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error)
	CompleteEffortSet         func(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error)
}

type ConfigurationCodexDeps struct {
	FetchModelList                   func(ctx context.Context) (codexrpc.ModelListResult, error)
	FetchPlanCollaborationModePreset func(ctx context.Context) (*codexrpc.CollaborationModeMask, error)
	UpdateGlobalModelConfig          func(mutate func(*config.CodexConfig), result codexrpc.ModelListResult) error
	RenderModelConfigCard            func(result codexrpc.ModelListResult, planPreset *codexrpc.CollaborationModeMask, sessionKey, menuAction string) map[string]any
}

type ConfigurationDeps struct {
	App        App
	Formatting ConfigurationFormattingDeps
	Commands   ConfigurationCommandDeps
	Claude     ConfigurationClaudeDeps
	Codex      ConfigurationCodexDeps
}

// NewConfigurationService creates a new ConfigurationService.
func NewConfigurationService(deps ConfigurationDeps) ConfigurationService {
	return ConfigurationService{App: deps.App, deps: deps}
}

func (s ConfigurationService) FormatMenuBody(action, body string) string {
	if s.deps.Formatting.FormatMenuBody == nil {
		return body
	}
	return s.deps.Formatting.FormatMenuBody(action, body)
}

func (s ConfigurationService) HandleModelCommand(msg *feishu.InboundMessage, args []string) error {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendCodex:
		if s.deps.Commands.HandleCodexModelCommand == nil {
			return fmt.Errorf("Codex model command handler not configured")
		}
		return s.deps.Commands.HandleCodexModelCommand(msg, args)
	case appruntime.BackendClaude:
		if s.deps.Commands.HandleClaudeModelCommand == nil {
			return fmt.Errorf("Claude model command handler not configured")
		}
		return s.deps.Commands.HandleClaudeModelCommand(msg, args)
	default:
		return unsupportedBackendError(appcore.ConfiguredBackend(s.App))
	}
}

func (s ConfigurationService) HandleWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error {
	if s.deps.Commands.HandleWorkspacePermissionCommand == nil {
		return fmt.Errorf("workspace permission handler not configured")
	}
	return s.deps.Commands.HandleWorkspacePermissionCommand(msg, args, sessionKey)
}

func (s ConfigurationService) CompleteClaudeModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	if s.deps.Claude.CompleteModelSet == nil {
		return nil, fmt.Errorf("Claude model set handler not configured")
	}
	return s.deps.Claude.CompleteModelSet(action, modelID)
}

func (s ConfigurationService) CompleteClaudeEffortSet(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error) {
	if s.deps.Claude.CompleteEffortSet == nil {
		return nil, fmt.Errorf("Claude effort set handler not configured")
	}
	return s.deps.Claude.CompleteEffortSet(action, effort)
}

func (s ConfigurationService) CompleteClaudeModelOptionAdd(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if appcore.ConfiguredBackend(s.App) != appruntime.BackendClaude {
		return unsupportedBackendActionResponse(appcore.ConfiguredBackend(s.App)), nil
	}
	if s.deps.Claude.CompleteModelOptionAdd == nil {
		return nil, fmt.Errorf("Claude model option add handler not configured")
	}
	return s.deps.Claude.CompleteModelOptionAdd(action)
}

func (s ConfigurationService) CompleteClaudeModelOptionRemove(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if appcore.ConfiguredBackend(s.App) != appruntime.BackendClaude {
		return unsupportedBackendActionResponse(appcore.ConfiguredBackend(s.App)), nil
	}
	if s.deps.Claude.CompleteModelOptionRemove == nil {
		return nil, fmt.Errorf("Claude model option remove handler not configured")
	}
	return s.deps.Claude.CompleteModelOptionRemove(action)
}

func (s ConfigurationService) FetchModelList(ctx context.Context) (codexrpc.ModelListResult, error) {
	if s.deps.Codex.FetchModelList == nil {
		return codexrpc.ModelListResult{}, fmt.Errorf("Codex model list fetcher not configured")
	}
	return s.deps.Codex.FetchModelList(ctx)
}

func (s ConfigurationService) UpdateGlobalModelConfig(mutate func(*config.CodexConfig), result codexrpc.ModelListResult) error {
	if s.deps.Codex.UpdateGlobalModelConfig == nil {
		return fmt.Errorf("Codex model config updater not configured")
	}
	return s.deps.Codex.UpdateGlobalModelConfig(mutate, result)
}

func (s ConfigurationService) RenderModelConfigCard(result codexrpc.ModelListResult, sessionKey, menuAction string) map[string]any {
	if s.deps.Codex.RenderModelConfigCard == nil {
		return nil
	}
	return s.deps.Codex.RenderModelConfigCard(result, s.fetchPlanCollaborationModePresetForRender(), sessionKey, menuAction)
}

func (s ConfigurationService) fetchPlanCollaborationModePresetForRender() *codexrpc.CollaborationModeMask {
	if s.deps.Codex.FetchPlanCollaborationModePreset == nil {
		return nil
	}
	cfg := s.App.Config()
	if cfg == nil || !cfg.Codex.ExperimentalAPI {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	preset, err := s.deps.Codex.FetchPlanCollaborationModePreset(ctx)
	if err != nil {
		return nil
	}
	return preset
}

// ---------------------------------------------------------------------------
// Pure helpers reimplemented in this package
// ---------------------------------------------------------------------------

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func submenuLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "›"
	}
	return label + " ›"
}

func commandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" {
		return slash
	}
	if slash == "" {
		return label
	}
	return label + " " + slash
}

func submenuCommandLabel(label, slash string) string {
	return submenuLabel(commandLabel(label, slash))
}

func normalizeClaudePermissionModeValue(value string) string {
	switch strings.TrimSpace(value) {
	case "", "default":
		return string(appruntime.ClaudePermissionModeDefault)
	case string(appruntime.ClaudePermissionModeAcceptEdits):
		return string(appruntime.ClaudePermissionModeAcceptEdits)
	case string(appruntime.ClaudePermissionModeBypass):
		return string(appruntime.ClaudePermissionModeBypass)
	case string(appruntime.ClaudePermissionModePlan):
		return string(appruntime.ClaudePermissionModePlan)
	default:
		return strings.TrimSpace(value)
	}
}

func effectiveClaudePermissionMode(sess *state.Session, ws *config.Workspace, cfg config.ClaudeConfig) string {
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(sess.ActiveClaudePermissionMode)
	}
	if ws != nil && strings.TrimSpace(ws.ClaudePermissionMode) != "" {
		return normalizeClaudePermissionModeValue(ws.ClaudePermissionMode)
	}
	return normalizeClaudePermissionModeValue(cfg.PermissionMode)
}

func claudePermissionModeLabel(value string) string {
	value = normalizeClaudePermissionModeValue(value)
	if value == "" {
		value = string(appruntime.ClaudePermissionModeDefault)
	}
	return "`" + value + "`"
}

func autoRetryEnabled(app App) bool {
	cfg := appcore.FeishuConfig(app)
	return cfg != nil && cfg.AutoRetry
}

func (s ConfigurationService) renderBackendRequiredCard(sessionKey string) map[string]any {
	body := s.FormatMenuBody("menu.group.model", unsupportedBackendUserMessage(appcore.ConfiguredBackend(s.App)))
	buttons := []feishu.Button{
		{Text: "后端选择 /backend", Type: "default", Value: cardactions.MenuActionValue{Action: "menu.group.backend", SessionKey: sessionKey}.Map()},
		{Text: "返回上一级", Type: "default", Value: cardactions.MenuActionValue{Action: "menu.root", SessionKey: sessionKey}.Map()},
	}
	return s.App.Feishu().SimpleStatusCard("模型配置", "orange", body, buttons)
}

func (s ConfigurationService) backendRequiredStatusBody() string {
	return strings.Join([]string{
		"backend: `" + firstNonEmpty(appcore.ConfiguredBackend(s.App), "unset") + "`",
		unsupportedBackendUserMessage(appcore.ConfiguredBackend(s.App)),
	}, "\n")
}

// ---------------------------------------------------------------------------
// Exported methods
// ---------------------------------------------------------------------------

// BackendWorkspaceCommandUsage returns the /workspace usage string for the
// active backend.
func (s ConfigurationService) BackendWorkspaceCommandUsage() string {
	return DriverForApp(s.App).Permission().WorkspaceCommandUsage()
}

// HandleBackendModelCommand dispatches model commands for the active backend.
func (s ConfigurationService) HandleBackendModelCommand(msg *feishu.InboundMessage, args []string) error {
	return s.HandleModelCommand(msg, args)
}

// HandleBackendWorkspacePermissionCommand dispatches workspace permission
// commands for the active backend.
func (s ConfigurationService) HandleBackendWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error {
	return s.HandleWorkspacePermissionCommand(msg, args, sessionKey)
}

// AppendBackendWorkspaceSummaryLines appends backend-specific workspace
// summary lines to the given slice.
func (s ConfigurationService) AppendBackendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string {
	return DriverForApp(s.App).Permission().AppendWorkspaceSummaryLines(s.App, lines, currentWS)
}

// BackendWorkspaceConfigButtons returns the workspace configuration buttons
// for the active backend.
func (s ConfigurationService) BackendWorkspaceConfigButtons(sessionKey string) []feishu.Button {
	return DriverForApp(s.App).Permission().WorkspaceConfigButtons(sessionKey)
}

// BackendWorkspaceSwitchInFlightNotice returns the notice text for a
// workspace switch that is in flight.
func (s ConfigurationService) BackendWorkspaceSwitchInFlightNotice() string {
	return DriverForApp(s.App).Conversation().WorkspaceSwitchInFlightNotice()
}

// BackendWorkspaceSwitchBindingFailureNotice returns the notice text for a
// workspace switch binding failure.
func (s ConfigurationService) BackendWorkspaceSwitchBindingFailureNotice() string {
	return DriverForApp(s.App).Conversation().WorkspaceSwitchBindingFailureNotice()
}

// BackendWorkspaceSwitchBindingNotice returns the notice text for a
// workspace switch binding result.
func (s ConfigurationService) BackendWorkspaceSwitchBindingNotice(binding *appworkspace.ThreadBinding) string {
	return DriverForApp(s.App).Conversation().WorkspaceSwitchBindingNotice(binding)
}

// RenderModelMenuCard renders the model menu card for the active backend.
func (s ConfigurationService) RenderModelMenuCard(sessionKey string) map[string]any {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendCodex:
		return s.RenderCodexModelMenuCard(sessionKey)
	case appruntime.BackendClaude:
		return s.RenderClaudeModelMenuCard(sessionKey)
	default:
		return s.renderBackendRequiredCard(sessionKey)
	}
}

// RenderClaudeModelMenuCard renders the Claude model menu card.
func (s ConfigurationService) RenderClaudeModelMenuCard(sessionKey string) map[string]any {
	cfg := s.App.Config()
	modelValue := firstNonEmpty(appmodelconfig.ConfiguredClaudeModel(cfg), appmodelconfig.ClaudeDefaultModelAlias)
	effortValue := firstNonEmpty(appmodelconfig.ConfiguredClaudeEffort(cfg), "(default)")
	body := strings.Join([]string{
		"当前 model: `" + modelValue + "`",
		"当前 effort: `" + effortValue + "`",
		"Claude model / effort 只允许在 frontend 空闲时切换。",
		"切换成功后会尝试立即应用到当前会话；后续新会话会使用新配置。",
	}, "\n")
	buttons := []feishu.Button{
		{Text: submenuCommandLabel("模型配置", "/model"), Type: "default", Value: cardactions.MenuActionValue{Action: "menu.model", SessionKey: sessionKey}.Map()},
		{Text: "返回上一级", Type: "default", Value: cardactions.MenuActionValue{Action: "menu.root", SessionKey: sessionKey}.Map()},
	}
	body = s.FormatMenuBody("menu.group.model", body)
	return s.App.Feishu().SimpleStatusCard("模型配置", "blue", body, buttons)
}

// RenderCodexModelMenuCard renders the Codex model menu card.
func (s ConfigurationService) RenderCodexModelMenuCard(sessionKey string) map[string]any {
	cfg := s.App.Config()
	modelValue := firstNonEmpty(appmodelconfig.ConfiguredGlobalModel(cfg), "(default)")
	effortValue := firstNonEmpty(appmodelconfig.ConfiguredGlobalReasoningEffort(cfg), "(default)")
	fastValue := "-"
	if store := s.App.Store(); store != nil {
		if sess := store.GetSession(strings.TrimSpace(sessionKey)); sess != nil {
			fastValue = appruntime.RenderServiceTierValue(sess.ActiveThreadServiceTier)
		}
	}
	body := strings.Join([]string{
		"当前 model: `" + modelValue + "`",
		"当前 reasoning: `" + effortValue + "`",
		"当前 fast: " + fastValue,
	}, "\n")
	buttons := []feishu.Button{
		{Text: submenuCommandLabel("模型配置", "/model"), Type: "default", Value: cardactions.MenuActionValue{Action: "menu.model", SessionKey: sessionKey}.Map()},
		{Text: submenuCommandLabel("响应速度", "/fast config"), Type: "default", Value: cardactions.MenuActionValue{Action: "menu.fast", SessionKey: sessionKey}.Map()},
		{Text: "返回上一级", Type: "default", Value: cardactions.MenuActionValue{Action: "menu.root", SessionKey: sessionKey}.Map()},
	}
	body = s.FormatMenuBody("menu.group.model", body)
	return s.App.Feishu().SimpleStatusCard("模型配置", "blue", body, buttons)
}

// CompleteGlobalModelSet completes a global model set action, dispatching
// by backend.
func (s ConfigurationService) CompleteGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendCodex:
		return s.CompleteCodexGlobalModelSet(action, modelID)
	case appruntime.BackendClaude:
		if s.deps.Claude.CompleteModelSet != nil {
			return s.CompleteClaudeModelSet(action, modelID)
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "Claude model set handler not configured"}}, nil
	default:
		return unsupportedBackendActionResponse(appcore.ConfiguredBackend(s.App)), nil
	}
}

// CompleteCodexGlobalModelSet completes a Codex global model set action.
func (s ConfigurationService) CompleteCodexGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := apputil.StringValue(action.ActionValue["session_key"])
	menuAction := apputil.StringValue(action.ActionValue["menu_action"])
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	if s.deps.Codex.FetchModelList == nil || s.deps.Codex.UpdateGlobalModelConfig == nil || s.deps.Codex.RenderModelConfigCard == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "Codex model operations not configured"}}, nil
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
		Toast: &callback.Toast{Type: "success", Content: "已更新全局模型"},
		Card:  RawCard(s.RenderModelConfigCard(result, sessionKey, menuAction)),
	}, nil
}

// CompleteGlobalReasoningEffortSet completes a global reasoning effort set
// action, dispatching by backend.
func (s ConfigurationService) CompleteGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendCodex:
		return s.CompleteCodexGlobalReasoningEffortSet(action, reasoningEffort)
	case appruntime.BackendClaude:
		if s.deps.Claude.CompleteEffortSet != nil {
			return s.CompleteClaudeEffortSet(action, reasoningEffort)
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "Claude effort set handler not configured"}}, nil
	default:
		return unsupportedBackendActionResponse(appcore.ConfiguredBackend(s.App)), nil
	}
}

// CompleteCodexGlobalReasoningEffortSet completes a Codex global reasoning
// effort set action.
func (s ConfigurationService) CompleteCodexGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	sessionKey := apputil.StringValue(action.ActionValue["session_key"])
	menuAction := apputil.StringValue(action.ActionValue["menu_action"])
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	if s.deps.Codex.FetchModelList == nil || s.deps.Codex.UpdateGlobalModelConfig == nil || s.deps.Codex.RenderModelConfigCard == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "Codex model operations not configured"}}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := s.FetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	selectedModel, _ := appmodelconfig.EffectiveConfiguredModelAndEffort(s.App.Config(), result)
	if strings.TrimSpace(reasoningEffort) != "" && !appmodelconfig.ModelSupportsEffort(selectedModel, reasoningEffort) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模型不支持这个推理强度"}}, nil
	}
	if err := s.UpdateGlobalModelConfig(func(c *config.CodexConfig) {
		c.ReasoningEffort = strings.TrimSpace(reasoningEffort)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局推理强度"},
		Card:  RawCard(s.RenderModelConfigCard(result, sessionKey, menuAction)),
	}, nil
}

// StatusCardBody returns the status card body text for the given session,
// dispatching by backend.
func (s ConfigurationService) StatusCardBody(sess *state.Session) string {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendCodex:
		return s.RenderCodexStatusBody(sess)
	case appruntime.BackendClaude:
		return s.RenderClaudeStatusBody(sess)
	default:
		return s.backendRequiredStatusBody()
	}
}

// RenderClaudeStatusBody renders the Claude status card body.
func (s ConfigurationService) RenderClaudeStatusBody(sess *state.Session) string {
	workspaceID := appcore.DefaultWorkspaceID(s.App)
	conversationLabel := "-"
	conversationID := "-"
	status := "idle"
	queueLen := 0
	var ws *config.Workspace
	if sess != nil {
		if strings.TrimSpace(sess.WorkspaceID) != "" {
			workspaceID = sess.WorkspaceID
		}
		conversationLabel = appthreadview.CurrentThreadLabel(sess.ActiveThreadName, sess.ActiveThreadPreview, sess.ActiveThreadID)
		conversationID = firstNonEmpty(sess.ActiveThreadID, "-")
		status = firstNonEmpty(sess.Status, "idle")
		queueLen = len(sess.Queue)
	}
	cfg := s.App.Config()
	ws = config.FindWorkspace(cfg, workspaceID)
	model := firstNonEmpty(appmodelconfig.ConfiguredClaudeModel(cfg), appmodelconfig.ClaudeDefaultModelAlias)
	effort := firstNonEmpty(appmodelconfig.ConfiguredClaudeEffort(cfg), "(follow Claude default)")
	feishuCfg := appcore.FeishuConfig(s.App)
	lines := []string{
		"状态: `" + status + "`",
		"backend: `" + firstNonEmpty(appcore.ConfiguredBackend(s.App), "unset") + "`",
		"版本: `" + buildinfo.CurrentVersion() + "`",
		"log level: " + appdebugview.RenderRuntimeLogLevelValue(),
		"工作区: `" + workspaceID + "`",
		"会话: " + conversationLabel,
		"session_id: `" + conversationID + "`",
		"Claude model: `" + model + "`",
		"Claude effort: `" + effort + "`",
		"auto retry: `" + map[bool]string{true: "on", false: "off"}[autoRetryEnabled(s.App)] + "`",
		"quiet: `" + appquietmode.StatusText(appquietmode.Mode(feishuCfg)) + "`",
		"queue_len: `" + fmt.Sprintf("%d", queueLen) + "`",
	}
	lines = DriverForApp(s.App).Permission().AppendStatusLines(s.App, lines[:len(lines)-1], sess, ws)
	lines = append(lines, "queue_len: `"+fmt.Sprintf("%d", queueLen)+"`")
	return strings.Join(lines, "\n")
}

// RenderCodexStatusBody renders the Codex status card body.
func (s ConfigurationService) RenderCodexStatusBody(sess *state.Session) string {
	workspaceID := appcore.DefaultWorkspaceID(s.App)
	conversationLabel := "-"
	conversationID := "-"
	status := "idle"
	queueLen := 0
	var ws *config.Workspace
	if sess != nil {
		if strings.TrimSpace(sess.WorkspaceID) != "" {
			workspaceID = sess.WorkspaceID
		}
		conversationLabel = appthreadview.CurrentThreadLabel(sess.ActiveThreadName, sess.ActiveThreadPreview, sess.ActiveThreadID)
		conversationID = firstNonEmpty(sess.ActiveThreadID, "-")
		status = firstNonEmpty(sess.Status, "idle")
		queueLen = len(sess.Queue)
	}
	cfg := s.App.Config()
	ws = config.FindWorkspace(cfg, workspaceID)
	model := appmodelconfig.ConfiguredGlobalModel(cfg)
	effort := appmodelconfig.ConfiguredGlobalReasoningEffort(cfg)
	if model == "" {
		model = "(follow app-server default)"
	}
	if effort == "" {
		effort = "(follow model default)"
	}
	feishuCfg := appcore.FeishuConfig(s.App)
	lines := []string{
		"状态: `" + status + "`",
		"backend: `" + firstNonEmpty(appcore.ConfiguredBackend(s.App), "unset") + "`",
		"版本: `" + buildinfo.CurrentVersion() + "`",
		"log level: " + appdebugview.RenderRuntimeLogLevelValue(),
		"工作区: `" + workspaceID + "`",
		"线程: " + conversationLabel,
		"thread_id: `" + conversationID + "`",
		"全局模型: `" + model + "`",
		"全局推理强度: `" + effort + "`",
		"auto retry: `" + map[bool]string{true: "on", false: "off"}[autoRetryEnabled(s.App)] + "`",
		"quiet: `" + appquietmode.StatusText(appquietmode.Mode(feishuCfg)) + "`",
		"queue_len: `" + fmt.Sprintf("%d", queueLen) + "`",
	}
	lines = DriverForApp(s.App).Permission().AppendStatusLines(s.App, lines[:len(lines)-1], sess, ws)
	lines = append(lines, "queue_len: `"+fmt.Sprintf("%d", queueLen)+"`")
	return strings.Join(lines, "\n")
}
