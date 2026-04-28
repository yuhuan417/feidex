package backend

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/app/appcore"
	appdebugview "feidex/internal/app/debugview"
	appmodelconfig "feidex/internal/app/modelconfig"
	appquietmode "feidex/internal/app/quietmode"
	appruntime "feidex/internal/app/runtime"
	appsessionctx "feidex/internal/app/sessionctx"
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
	HandleModelCommand               func(msg *feishu.InboundMessage, args []string) error
	HandleWorkspacePermissionCommand func(msg *feishu.InboundMessage, args []string, sessionKey string) error
}

type ConfigurationClaudeDeps struct {
	CompleteModelSet  func(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error)
	CompleteEffortSet func(action *feishu.CardAction, effort string) (*callback.CardActionTriggerResponse, error)
}

type ConfigurationCodexDeps struct {
	FetchModelList          func(ctx context.Context) (codexrpc.ModelListResult, error)
	UpdateGlobalModelConfig func(mutate func(*config.CodexConfig), result codexrpc.ModelListResult) error
	RenderModelConfigCard   func(result codexrpc.ModelListResult, sessionKey, menuAction string) map[string]any
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
	if s.deps.Commands.HandleModelCommand == nil {
		return fmt.Errorf("model command handler not configured")
	}
	return s.deps.Commands.HandleModelCommand(msg, args)
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
	return s.deps.Codex.RenderModelConfigCard(result, sessionKey, menuAction)
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

// ---------------------------------------------------------------------------
// Exported methods
// ---------------------------------------------------------------------------

// BackendWorkspaceCommandUsage returns the /workspace usage string for the
// active backend.
func (s ConfigurationService) BackendWorkspaceCommandUsage() string {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		return ClaudeWorkspaceCommandUsage
	default:
		return appworkspace.CommandUsage
	}
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
	if currentWS == nil {
		return lines
	}
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		effectiveMode := effectiveClaudePermissionMode(nil, currentWS, s.App.Config().Claude)
		override := strings.TrimSpace(currentWS.ClaudePermissionMode)
		overrideLabel := "跟随全局"
		if override != "" {
			overrideLabel = claudePermissionModeLabel(override)
		}
		return append(lines,
			"默认 Claude 权限: "+claudePermissionModeLabel(effectiveMode),
			"工作区覆盖: "+overrideLabel,
		)
	default:
		return append(lines,
			"默认 sandbox: `"+currentWS.SandboxMode+"`",
			"默认 policy: `"+currentWS.ApprovalPolicy+"`",
		)
	}
}

// BackendWorkspaceConfigButtons returns the workspace configuration buttons
// for the active backend.
func (s ConfigurationService) BackendWorkspaceConfigButtons(sessionKey string) []feishu.Button {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		return []feishu.Button{{
			Text: submenuCommandLabel("默认权限", "/workspace permissions"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.permission_mode.menu",
				"session_key": sessionKey,
			},
		}}
	default:
		return []feishu.Button{
			{
				Text: submenuCommandLabel("配置默认沙箱", "/workspace sandbox"),
				Type: "default",
				Value: map[string]any{
					"action":      "workspace.sandbox.menu",
					"session_key": sessionKey,
				},
			},
			{
				Text: submenuCommandLabel("配置默认策略", "/workspace policy"),
				Type: "default",
				Value: map[string]any{
					"action":      "workspace.policy.menu",
					"session_key": sessionKey,
				},
			},
		}
	}
}

// BackendWorkspaceSwitchInFlightNotice returns the notice text for a
// workspace switch that is in flight.
func (s ConfigurationService) BackendWorkspaceSwitchInFlightNotice() string {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		return "。当前运行中的任务仍归属原会话；后续新任务会使用新工作区。"
	default:
		return "。当前运行中的任务仍归属原线程；后续新任务会使用新工作区。"
	}
}

// BackendWorkspaceSwitchBindingFailureNotice returns the notice text for a
// workspace switch binding failure.
func (s ConfigurationService) BackendWorkspaceSwitchBindingFailureNotice() string {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		return "。自动绑定会话失败，可稍后重试。"
	default:
		return "。自动绑定 thread 失败，可稍后重试。"
	}
}

// BackendWorkspaceSwitchBindingNotice returns the notice text for a
// workspace switch binding result.
func (s ConfigurationService) BackendWorkspaceSwitchBindingNotice(binding *appworkspace.ThreadBinding) string {
	resumed := binding != nil && binding.Resumed
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		if resumed {
			return "。已自动恢复该工作区最近使用的会话。"
		}
		return "。已自动创建新会话。"
	default:
		if resumed {
			return "。已自动恢复该工作区最近使用的线程。"
		}
		return "。已自动创建新线程。"
	}
}

// RenderModelMenuCard renders the model menu card for the active backend.
func (s ConfigurationService) RenderModelMenuCard(sessionKey string) map[string]any {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		return s.RenderClaudeModelMenuCard(sessionKey)
	default:
		return s.RenderCodexModelMenuCard(sessionKey)
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
		{Text: submenuCommandLabel("模型配置", "/model"), Type: "default", Value: map[string]any{"action": "menu.model", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}},
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
		{Text: submenuCommandLabel("模型配置", "/model"), Type: "default", Value: map[string]any{"action": "menu.model", "session_key": sessionKey}},
		{Text: submenuCommandLabel("响应速度", "/fast config"), Type: "default", Value: map[string]any{"action": "menu.fast", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.root", "session_key": sessionKey}},
	}
	body = s.FormatMenuBody("menu.group.model", body)
	return s.App.Feishu().SimpleStatusCard("模型配置", "blue", body, buttons)
}

// CompleteGlobalModelSet completes a global model set action, dispatching
// by backend.
func (s ConfigurationService) CompleteGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	switch appcore.ConfiguredBackend(s.App) {
	case appruntime.BackendClaude:
		if s.deps.Claude.CompleteModelSet != nil {
			return s.CompleteClaudeModelSet(action, modelID)
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "Claude model set handler not configured"}}, nil
	default:
		return s.CompleteCodexGlobalModelSet(action, modelID)
	}
}

// CompleteCodexGlobalModelSet completes a Codex global model set action.
func (s ConfigurationService) CompleteCodexGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	menuAction, _ := action.ActionValue["menu_action"].(string)
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
	case appruntime.BackendClaude:
		if s.deps.Claude.CompleteEffortSet != nil {
			return s.CompleteClaudeEffortSet(action, reasoningEffort)
		}
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "Claude effort set handler not configured"}}, nil
	default:
		return s.CompleteCodexGlobalReasoningEffortSet(action, reasoningEffort)
	}
}

// CompleteCodexGlobalReasoningEffortSet completes a Codex global reasoning
// effort set action.
func (s ConfigurationService) CompleteCodexGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	menuAction, _ := action.ActionValue["menu_action"].(string)
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
	case appruntime.BackendClaude:
		return s.RenderClaudeStatusBody(sess)
	default:
		return s.RenderCodexStatusBody(sess)
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
	workspacePermission := "-"
	sessionPermission := "跟随工作区"
	effectivePermission := "-"
	if ws != nil {
		workspacePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(nil, ws, cfg.Claude))
		effectivePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(sess, ws, cfg.Claude))
	}
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		sessionPermission = claudePermissionModeLabel(sess.ActiveClaudePermissionMode)
	}
	feishuCfg := appcore.FeishuConfig(s.App)
	return strings.Join([]string{
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
		"workspace permission mode: " + workspacePermission,
		"session permission mode: " + sessionPermission,
		"effective permission mode: " + effectivePermission,
		"queue_len: `" + fmt.Sprintf("%d", queueLen) + "`",
	}, "\n")
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
	workspaceSandbox := "-"
	workspacePolicy := "-"
	effectiveSandbox := "-"
	effectivePolicy := "-"
	if ws != nil {
		workspaceSandbox = firstNonEmpty(ws.SandboxMode, "-")
		workspacePolicy = firstNonEmpty(ws.ApprovalPolicy, "-")
		effectiveSandbox = appsessionctx.EffectiveSandboxMode(sess, ws)
		effectivePolicy = appsessionctx.EffectiveApprovalPolicy(sess, ws)
	}
	threadSandbox := appthreadview.RenderThreadSettingValue("", "")
	threadPolicy := appthreadview.RenderThreadSettingValue("", "")
	threadServiceTier := "-"
	if sess != nil {
		threadSandbox = appthreadview.RenderThreadSettingValue(sess.ActiveThreadSandboxMode, "")
		threadPolicy = appthreadview.RenderThreadSettingValue(sess.ActiveThreadApprovalPolicy, "")
		threadServiceTier = appruntime.RenderServiceTierValue(sess.ActiveThreadServiceTier)
	}
	feishuCfg := appcore.FeishuConfig(s.App)
	return strings.Join([]string{
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
		"workspace sandbox: `" + workspaceSandbox + "`",
		"workspace policy: `" + workspacePolicy + "`",
		"thread sandbox: " + threadSandbox,
		"thread policy: " + threadPolicy,
		"thread service tier: " + threadServiceTier,
		"生效 sandbox: `" + effectiveSandbox + "`",
		"生效 policy: `" + effectivePolicy + "`",
		"queue_len: `" + fmt.Sprintf("%d", queueLen) + "`",
	}, "\n")
}
