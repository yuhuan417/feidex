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

func (a *App) backendWorkspaceCommandUsage() string {
	return a.backendConfiguration().workspaceCommandUsage()
}

func (a *App) handleBackendModelCommand(msg *feishu.InboundMessage, args []string) error {
	return a.backendConfiguration().handleModelCommand(msg, args)
}

func (a *App) handleBackendWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error {
	config := a.backendConfiguration()
	if !config.supportsWorkspacePermissionMode() {
		return fmt.Errorf("usage: %s", config.workspaceCommandUsage())
	}
	return config.handleWorkspacePermissionCommand(msg, args, sessionKey)
}

func (a *App) appendBackendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string {
	return a.backendConfiguration().appendWorkspaceSummaryLines(lines, currentWS)
}

func (a *App) backendWorkspaceConfigButtons(sessionKey string) []feishu.Button {
	return a.backendConfiguration().workspaceConfigButtons(sessionKey)
}

func (a *App) backendWorkspaceSwitchInFlightNotice() string {
	return a.backendConfiguration().workspaceSwitchInFlightNotice()
}

func (a *App) backendWorkspaceSwitchBindingFailureNotice() string {
	return a.backendConfiguration().workspaceSwitchBindingFailureNotice()
}

func (a *App) backendWorkspaceSwitchBindingNotice(binding *workspaceThreadBinding) string {
	return a.backendConfiguration().workspaceSwitchBindingNotice(binding)
}

func (a *App) renderModelMenuCard(sessionKey string) map[string]any {
	return a.backendConfiguration().renderModelMenuCard(sessionKey)
}

func (a *App) renderClaudeModelMenuCard(sessionKey string) map[string]any {
	modelValue := firstNonEmpty(configuredClaudeModel(a.cfg), claudeDefaultModelAlias)
	effortValue := firstNonEmpty(configuredClaudeEffort(a.cfg), "(default)")
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
	return a.feishu.SimpleStatusCard("模型配置", "blue", menuCardBody("menu.group.model", body), buttons)
}

func (a *App) renderCodexModelMenuCard(sessionKey string) map[string]any {
	modelValue := firstNonEmpty(configuredGlobalModel(a.cfg), "(default)")
	effortValue := firstNonEmpty(configuredGlobalReasoningEffort(a.cfg), "(default)")
	fastValue := "-"
	if a.store != nil {
		if sess := a.appState().session(sessionKey); sess != nil {
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
	return a.feishu.SimpleStatusCard("模型配置", "blue", menuCardBody("menu.group.model", body), buttons)
}

func (a *App) completeGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	return a.backendConfiguration().completeGlobalModelSet(action, modelID)
}

func (a *App) completeCodexGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	menuAction, _ := action.ActionValue["menu_action"].(string)
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if err := a.updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.Model = strings.TrimSpace(modelID)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局模型"},
		Card:  rawCard(a.renderModelConfigCard(result, sessionKey, menuAction)),
	}, nil
}

func (a *App) completeGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	return a.backendConfiguration().completeGlobalReasoningEffortSet(action, reasoningEffort)
}

func (a *App) completeCodexGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	sessionKey, _ := action.ActionValue["session_key"].(string)
	menuAction, _ := action.ActionValue["menu_action"].(string)
	if strings.TrimSpace(menuAction) == "" {
		menuAction = "menu.model"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := a.fetchModelList(ctx)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	selectedModel, _ := effectiveConfiguredModelAndEffort(a.cfg, result)
	if strings.TrimSpace(reasoningEffort) != "" && !modelSupportsEffort(selectedModel, reasoningEffort) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前模型不支持这个推理强度"}}, nil
	}
	if err := a.updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.ReasoningEffort = strings.TrimSpace(reasoningEffort)
	}, result); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新全局推理强度"},
		Card:  rawCard(a.renderModelConfigCard(result, sessionKey, menuAction)),
	}, nil
}

func (a *App) statusCardBody(sess *state.Session) string {
	return a.backendConfiguration().statusCardBody(sess)
}

func (a *App) renderClaudeStatusBody(sess *state.Session) string {
	workspaceID := a.defaultWorkspaceID()
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
	ws = config.FindWorkspace(a.cfg, workspaceID)
	model := firstNonEmpty(configuredClaudeModel(a.cfg), claudeDefaultModelAlias)
	effort := firstNonEmpty(configuredClaudeEffort(a.cfg), "(follow Claude default)")
	workspacePermission := "-"
	sessionPermission := "跟随工作区"
	effectivePermission := "-"
	if ws != nil {
		workspacePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(nil, ws, a.cfg.Claude))
		effectivePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(sess, ws, a.cfg.Claude))
	}
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		sessionPermission = claudePermissionModeLabel(sess.ActiveClaudePermissionMode)
	}
	return strings.Join([]string{
		"状态: `" + status + "`",
		"backend: `" + firstNonEmpty(a.configuredBackend(), "unset") + "`",
		"版本: `" + currentVersion() + "`",
		"log level: " + renderRuntimeLogLevelValue(),
		"工作区: `" + workspaceID + "`",
		"会话: " + conversationLabel,
		"session_id: `" + conversationID + "`",
		"Claude model: `" + model + "`",
		"Claude effort: `" + effort + "`",
		"auto retry: `" + map[bool]string{true: "on", false: "off"}[a.autoRetryEnabled()] + "`",
		"quiet: `" + quietModeStatusText(a.quietMode()) + "`",
		"workspace permission mode: " + workspacePermission,
		"session permission mode: " + sessionPermission,
		"effective permission mode: " + effectivePermission,
		"queue_len: `" + fmt.Sprintf("%d", queueLen) + "`",
	}, "\n")
}

func (a *App) renderCodexStatusBody(sess *state.Session) string {
	workspaceID := a.defaultWorkspaceID()
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
	ws = config.FindWorkspace(a.cfg, workspaceID)
	model := configuredGlobalModel(a.cfg)
	effort := configuredGlobalReasoningEffort(a.cfg)
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
		"backend: `" + firstNonEmpty(a.configuredBackend(), "unset") + "`",
		"版本: `" + currentVersion() + "`",
		"log level: " + renderRuntimeLogLevelValue(),
		"工作区: `" + workspaceID + "`",
		"线程: " + conversationLabel,
		"thread_id: `" + conversationID + "`",
		"全局模型: `" + model + "`",
		"全局推理强度: `" + effort + "`",
		"auto retry: `" + map[bool]string{true: "on", false: "off"}[a.autoRetryEnabled()] + "`",
		"quiet: `" + quietModeStatusText(a.quietMode()) + "`",
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
