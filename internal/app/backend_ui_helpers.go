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
	if a != nil && a.isClaudeBackend() {
		return claudeWorkspaceCommandUsage
	}
	return workspaceCommandUsage
}

func (a *App) handleBackendModelCommand(msg *feishu.InboundMessage, args []string) error {
	if a != nil && a.isClaudeBackend() {
		return a.commandClaudeModel(msg, args)
	}
	return a.commandCodexModel(msg, args)
}

func (a *App) backendSupportsWorkspacePermissionMode() bool {
	return a != nil && a.isClaudeBackend()
}

func (a *App) handleBackendWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error {
	if !a.backendSupportsWorkspacePermissionMode() {
		return fmt.Errorf("usage: %s", a.backendWorkspaceCommandUsage())
	}
	if len(args) == 1 {
		return a.showClaudeWorkspacePermissionMenu(msg)
	}
	if len(args) != 2 {
		return fmt.Errorf("usage: /workspace permissions [MODE|inherit]")
	}
	_, _, ws := a.currentWorkspaceForMessage(msg)
	if ws == nil {
		return fmt.Errorf("workspace not found")
	}
	resp, err := a.completeClaudeWorkspacePermissionModeSet(a.commandActionFromMessage(msg, nil), sessionKey, ws.ID, strings.TrimSpace(args[1]))
	if err != nil {
		return err
	}
	return a.replyCommandActionResponse(msg, resp)
}

func (a *App) appendBackendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string {
	if currentWS == nil {
		return lines
	}
	if a != nil && a.isClaudeBackend() {
		effectiveMode := effectiveClaudePermissionMode(nil, currentWS, a.cfg.Claude)
		override := strings.TrimSpace(currentWS.ClaudePermissionMode)
		overrideLabel := "跟随全局"
		if override != "" {
			overrideLabel = claudePermissionModeLabel(override)
		}
		return append(lines,
			"默认 Claude 权限: "+claudePermissionModeLabel(effectiveMode),
			"工作区覆盖: "+overrideLabel,
		)
	}
	return append(lines,
		"默认 sandbox: `"+currentWS.SandboxMode+"`",
		"默认 policy: `"+currentWS.ApprovalPolicy+"`",
	)
}

func (a *App) backendWorkspaceConfigButtons(sessionKey string) []feishu.Button {
	if a != nil && a.isClaudeBackend() {
		return []feishu.Button{{
			Text: submenuCommandLabel("默认权限", "/workspace permissions"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.permission_mode.menu",
				"session_key": sessionKey,
			},
		}}
	}
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

func (a *App) backendWorkspaceSwitchInFlightNotice() string {
	if a != nil && a.isClaudeBackend() {
		return "。当前运行中的任务仍归属原会话；后续新任务会使用新工作区。"
	}
	return "。当前运行中的任务仍归属原线程；后续新任务会使用新工作区。"
}

func (a *App) backendWorkspaceSwitchBindingFailureNotice() string {
	if a != nil && a.isClaudeBackend() {
		return "。自动绑定会话失败，可稍后重试。"
	}
	return "。自动绑定 thread 失败，可稍后重试。"
}

func (a *App) backendWorkspaceSwitchBindingNotice(binding *workspaceThreadBinding) string {
	if binding != nil && binding.Resumed {
		if a != nil && a.isClaudeBackend() {
			return "。已自动恢复该工作区最近使用的会话。"
		}
		return "。已自动恢复该工作区最近使用的线程。"
	}
	if a != nil && a.isClaudeBackend() {
		return "。已自动创建新会话。"
	}
	return "。已自动创建新线程。"
}

func (a *App) renderModelMenuCard(sessionKey string) map[string]any {
	if a != nil && a.isClaudeBackend() {
		return a.renderClaudeModelMenuCard(sessionKey)
	}
	return a.renderCodexModelMenuCard(sessionKey)
}

func (a *App) renderClaudeModelMenuCard(sessionKey string) map[string]any {
	modelValue := firstNonEmpty(configuredClaudeModel(a.cfg), claudeDefaultModelAlias)
	effortValue := firstNonEmpty(configuredClaudeEffort(a.cfg), "(default)")
	body := strings.Join([]string{
		"当前 model: `" + modelValue + "`",
		"当前 effort: `" + effortValue + "`",
		"Claude model / effort 只允许在 frontend 空闲时切换。",
		"切换成功后会立即重置当前 frontend 的 Claude 会话。",
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
	if a != nil && a.isClaudeBackend() {
		return a.completeClaudeModelSet(action, modelID)
	}
	return a.completeCodexGlobalModelSet(action, modelID)
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
	if a != nil && a.isClaudeBackend() {
		return a.completeClaudeEffortSet(action, reasoningEffort)
	}
	return a.completeCodexGlobalReasoningEffortSet(action, reasoningEffort)
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
	if a != nil && a.isClaudeBackend() {
		return a.renderClaudeStatusBody(sess)
	}
	return a.renderCodexStatusBody(sess)
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
