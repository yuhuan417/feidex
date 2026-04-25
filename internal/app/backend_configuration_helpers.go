package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type backendConfigurationService struct {
	app *App
}
func newBackendConfigurationService(app *App) backendConfigurationService {
	return backendConfigurationService{app: app}
}

func (s backendConfigurationService) backendWorkspaceCommandUsage() string {
	switch configuredBackend(s.app) {
	case backendClaude:
		return claudeWorkspaceCommandUsage
	default:
		return workspaceCommandUsage
	}
}

func (s backendConfigurationService) handleBackendModelCommand(msg *feishu.InboundMessage, args []string) error {
	switch configuredBackend(s.app) {
	case backendClaude:
		return newModelConfigService(s.app).commandClaudeModel(msg, args)
	default:
		return newModelConfigService(s.app).commandCodexModel(msg, args)
	}
}

func (s backendConfigurationService) handleBackendWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error {
	switch configuredBackend(s.app) {
	case backendClaude:
		if len(args) == 1 {
			return showClaudeWorkspacePermissionMenu(s.app, msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /workspace permissions [MODE|inherit]")
		}
		_, _, ws := newWorkspaceConfigService(s.app).currentWorkspaceForMessage(msg)
		if ws == nil {
			return fmt.Errorf("workspace not found")
		}
		resp, err := newWorkspaceService(s.app).completeClaudeWorkspacePermissionModeSet(commandActionFromMessage(msg, nil), sessionKey, ws.ID, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return replyCommandActionResponse(s.app, msg, resp)
	default:
		return fmt.Errorf("usage: %s", workspaceCommandUsage)
	}
}

func (s backendConfigurationService) appendBackendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string {
	if currentWS == nil {
		return lines
	}
	switch configuredBackend(s.app) {
	case backendClaude:
		effectiveMode := effectiveClaudePermissionMode(nil, currentWS, s.app.cfg.Claude)
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

func (s backendConfigurationService) backendWorkspaceConfigButtons(sessionKey string) []feishu.Button {
	switch configuredBackend(s.app) {
	case backendClaude:
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

func (s backendConfigurationService) backendWorkspaceSwitchInFlightNotice() string {
	switch configuredBackend(s.app) {
	case backendClaude:
		return "。当前运行中的任务仍归属原会话；后续新任务会使用新工作区。"
	default:
		return "。当前运行中的任务仍归属原线程；后续新任务会使用新工作区。"
	}
}

func (s backendConfigurationService) backendWorkspaceSwitchBindingFailureNotice() string {
	switch configuredBackend(s.app) {
	case backendClaude:
		return "。自动绑定会话失败，可稍后重试。"
	default:
		return "。自动绑定 thread 失败，可稍后重试。"
	}
}

func (s backendConfigurationService) backendWorkspaceSwitchBindingNotice(binding *workspaceThreadBinding) string {
	switch configuredBackend(s.app) {
	case backendClaude:
		if binding != nil && binding.Resumed {
			return "。已自动恢复该工作区最近使用的会话。"
		}
		return "。已自动创建新会话。"
	default:
		if binding != nil && binding.Resumed {
			return "。已自动恢复该工作区最近使用的线程。"
		}
		return "。已自动创建新线程。"
	}
}

func (s backendConfigurationService) renderModelMenuCard(sessionKey string) map[string]any {
	switch configuredBackend(s.app) {
	case backendClaude:
		return s.renderClaudeModelMenuCard(sessionKey)
	default:
		return s.renderCodexModelMenuCard(sessionKey)
	}
}

func (s backendConfigurationService) renderClaudeModelMenuCard(sessionKey string) map[string]any {
	modelValue := firstNonEmpty(configuredClaudeModel(s.app.cfg), claudeDefaultModelAlias)
	effortValue := firstNonEmpty(configuredClaudeEffort(s.app.cfg), "(default)")
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
	return s.app.feishu.SimpleStatusCard("模型配置", "blue", menuCardBody("menu.group.model", body), buttons)
}

func (s backendConfigurationService) renderCodexModelMenuCard(sessionKey string) map[string]any {
	modelValue := firstNonEmpty(configuredGlobalModel(s.app.cfg), "(default)")
	effortValue := firstNonEmpty(configuredGlobalReasoningEffort(s.app.cfg), "(default)")
	fastValue := "-"
	if s.app.store != nil {
		if sess := appState(s.app).session(sessionKey); sess != nil {
			fastValue = renderServiceTierValue(sess.ActiveThreadServiceTier)
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
	return s.app.feishu.SimpleStatusCard("模型配置", "blue", menuCardBody("menu.group.model", body), buttons)
}

func (s backendConfigurationService) completeGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	switch configuredBackend(s.app) {
	case backendClaude:
		return newModelConfigService(s.app).completeClaudeModelSet(action, modelID)
	default:
		return s.completeCodexGlobalModelSet(action, modelID)
	}
}

func (s backendConfigurationService) completeCodexGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	menuAction, _ := action.ActionValue["menu_action"].(string)
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := newModelConfigService(s.app).fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if err := newModelConfigService(s.app).updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.Model = strings.TrimSpace(modelID)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局模型"},
		Card:  rawCard(newModelConfigService(s.app).renderModelConfigCard(result, sessionKey, menuAction)),
	}, nil
}

func (s backendConfigurationService) completeGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	switch configuredBackend(s.app) {
	case backendClaude:
		return newModelConfigService(s.app).completeClaudeEffortSet(action, reasoningEffort)
	default:
		return s.completeCodexGlobalReasoningEffortSet(action, reasoningEffort)
	}
}

func (s backendConfigurationService) completeCodexGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	menuAction, _ := action.ActionValue["menu_action"].(string)
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := newModelConfigService(s.app).fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	selectedModel, _ := effectiveConfiguredModelAndEffort(s.app.cfg, result)
	if strings.TrimSpace(reasoningEffort) != "" && !modelSupportsEffort(selectedModel, reasoningEffort) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模型不支持这个推理强度"}}, nil
	}
	if err := newModelConfigService(s.app).updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.ReasoningEffort = strings.TrimSpace(reasoningEffort)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局推理强度"},
		Card:  rawCard(newModelConfigService(s.app).renderModelConfigCard(result, sessionKey, menuAction)),
	}, nil
}

func (s backendConfigurationService) statusCardBody(sess *state.Session) string {
	switch configuredBackend(s.app) {
	case backendClaude:
		return s.renderClaudeStatusBody(sess)
	default:
		return s.renderCodexStatusBody(sess)
	}
}

func (s backendConfigurationService) renderClaudeStatusBody(sess *state.Session) string {
	workspaceID := defaultWorkspaceID(s.app)
	conversationLabel := "-"
	conversationID := "-"
	status := "idle"
	queueLen := 0
	var ws *config.Workspace
	if sess != nil {
		if strings.TrimSpace(sess.WorkspaceID) != "" {
			workspaceID = sess.WorkspaceID
		}
		conversationLabel = currentThreadLabel(sess)
		conversationID = firstNonEmpty(sess.ActiveThreadID, "-")
		status = firstNonEmpty(sess.Status, "idle")
		queueLen = len(sess.Queue)
	}
	ws = config.FindWorkspace(s.app.cfg, workspaceID)
	model := firstNonEmpty(configuredClaudeModel(s.app.cfg), claudeDefaultModelAlias)
	effort := firstNonEmpty(configuredClaudeEffort(s.app.cfg), "(follow Claude default)")
	workspacePermission := "-"
	sessionPermission := "跟随工作区"
	effectivePermission := "-"
	if ws != nil {
		workspacePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(nil, ws, s.app.cfg.Claude))
		effectivePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(sess, ws, s.app.cfg.Claude))
	}
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		sessionPermission = claudePermissionModeLabel(sess.ActiveClaudePermissionMode)
	}
	return strings.Join([]string{
		"状态: `" + status + "`",
		"backend: `" + firstNonEmpty(configuredBackend(s.app), "unset") + "`",
		"版本: `" + currentVersion() + "`",
		"log level: " + renderRuntimeLogLevelValue(),
		"工作区: `" + workspaceID + "`",
		"会话: " + conversationLabel,
		"session_id: `" + conversationID + "`",
		"Claude model: `" + model + "`",
		"Claude effort: `" + effort + "`",
		"auto retry: `" + map[bool]string{true: "on", false: "off"}[newAutoRetryService(s.app).autoRetryEnabled()] + "`",
		"quiet: `" + quietModeStatusText(quietMode(feishuConfig(s.app))) + "`",
		"workspace permission mode: " + workspacePermission,
		"session permission mode: " + sessionPermission,
		"effective permission mode: " + effectivePermission,
		"queue_len: `" + fmt.Sprintf("%d", queueLen) + "`",
	}, "\n")
}

func (s backendConfigurationService) renderCodexStatusBody(sess *state.Session) string {
	workspaceID := defaultWorkspaceID(s.app)
	conversationLabel := "-"
	conversationID := "-"
	status := "idle"
	queueLen := 0
	var ws *config.Workspace
	if sess != nil {
		if strings.TrimSpace(sess.WorkspaceID) != "" {
			workspaceID = sess.WorkspaceID
		}
		conversationLabel = currentThreadLabel(sess)
		conversationID = firstNonEmpty(sess.ActiveThreadID, "-")
		status = firstNonEmpty(sess.Status, "idle")
		queueLen = len(sess.Queue)
	}
	ws = config.FindWorkspace(s.app.cfg, workspaceID)
	model := configuredGlobalModel(s.app.cfg)
	effort := configuredGlobalReasoningEffort(s.app.cfg)
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
		effectiveSandbox = effectiveThreadSandboxMode(sess, ws)
		effectivePolicy = effectiveThreadApprovalPolicy(sess, ws)
	}
	threadSandbox := renderThreadSettingValue("", "")
	threadPolicy := renderThreadSettingValue("", "")
	threadServiceTier := "-"
	if sess != nil {
		threadSandbox = renderThreadSettingValue(sess.ActiveThreadSandboxMode, "")
		threadPolicy = renderThreadSettingValue(sess.ActiveThreadApprovalPolicy, "")
		threadServiceTier = renderServiceTierValue(sess.ActiveThreadServiceTier)
	}
	return strings.Join([]string{
		"状态: `" + status + "`",
		"backend: `" + firstNonEmpty(configuredBackend(s.app), "unset") + "`",
		"版本: `" + currentVersion() + "`",
		"log level: " + renderRuntimeLogLevelValue(),
		"工作区: `" + workspaceID + "`",
		"线程: " + conversationLabel,
		"thread_id: `" + conversationID + "`",
		"全局模型: `" + model + "`",
		"全局推理强度: `" + effort + "`",
		"auto retry: `" + map[bool]string{true: "on", false: "off"}[newAutoRetryService(s.app).autoRetryEnabled()] + "`",
		"quiet: `" + quietModeStatusText(quietMode(feishuConfig(s.app))) + "`",
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
