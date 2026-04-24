package app

import (
	"fmt"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type backendConfigurationFacade interface {
	workspaceCommandUsage() string
	handleModelCommand(msg *feishu.InboundMessage, args []string) error
	supportsWorkspacePermissionMode() bool
	handleWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error
	appendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string
	workspaceConfigButtons(sessionKey string) []feishu.Button
	workspaceSwitchInFlightNotice() string
	workspaceSwitchBindingFailureNotice() string
	workspaceSwitchBindingNotice(binding *workspaceThreadBinding) string
	renderModelMenuCard(sessionKey string) map[string]any
	completeGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error)
	completeGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error)
	statusCardBody(sess *state.Session) string
}

func backendConfiguration(a *App) backendConfigurationFacade {
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.configuration(a)
	}
	return codexBackendConfigurationFacade{app: a}
}

type codexBackendConfigurationFacade struct {
	app *App
}

func (c codexBackendConfigurationFacade) workspaceCommandUsage() string {
	return workspaceCommandUsage
}

func (c codexBackendConfigurationFacade) handleModelCommand(msg *feishu.InboundMessage, args []string) error {
	return newModelConfigService(c.app).commandCodexModel(msg, args)
}

func (codexBackendConfigurationFacade) supportsWorkspacePermissionMode() bool {
	return false
}

func (c codexBackendConfigurationFacade) handleWorkspacePermissionCommand(*feishu.InboundMessage, []string, string) error {
	return fmt.Errorf("usage: %s", c.workspaceCommandUsage())
}

func (codexBackendConfigurationFacade) appendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string {
	if currentWS == nil {
		return lines
	}
	return append(lines,
		"默认 sandbox: `"+currentWS.SandboxMode+"`",
		"默认 policy: `"+currentWS.ApprovalPolicy+"`",
	)
}

func (codexBackendConfigurationFacade) workspaceConfigButtons(sessionKey string) []feishu.Button {
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

func (codexBackendConfigurationFacade) workspaceSwitchInFlightNotice() string {
	return "。当前运行中的任务仍归属原线程；后续新任务会使用新工作区。"
}

func (codexBackendConfigurationFacade) workspaceSwitchBindingFailureNotice() string {
	return "。自动绑定 thread 失败，可稍后重试。"
}

func (codexBackendConfigurationFacade) workspaceSwitchBindingNotice(binding *workspaceThreadBinding) string {
	if binding != nil && binding.Resumed {
		return "。已自动恢复该工作区最近使用的线程。"
	}
	return "。已自动创建新线程。"
}

func (c codexBackendConfigurationFacade) renderModelMenuCard(sessionKey string) map[string]any {
	return newBackendConfigurationService(c.app).renderCodexModelMenuCard(sessionKey)
}

func (c codexBackendConfigurationFacade) completeGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	return newBackendConfigurationService(c.app).completeCodexGlobalModelSet(action, modelID)
}

func (c codexBackendConfigurationFacade) completeGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	return newBackendConfigurationService(c.app).completeCodexGlobalReasoningEffortSet(action, reasoningEffort)
}

func (c codexBackendConfigurationFacade) statusCardBody(sess *state.Session) string {
	return newBackendConfigurationService(c.app).renderCodexStatusBody(sess)
}

type claudeBackendConfigurationFacade struct {
	app *App
}

func (claudeBackendConfigurationFacade) workspaceCommandUsage() string {
	return claudeWorkspaceCommandUsage
}

func (c claudeBackendConfigurationFacade) handleModelCommand(msg *feishu.InboundMessage, args []string) error {
	return newModelConfigService(c.app).commandClaudeModel(msg, args)
}

func (claudeBackendConfigurationFacade) supportsWorkspacePermissionMode() bool {
	return true
}

func (c claudeBackendConfigurationFacade) handleWorkspacePermissionCommand(msg *feishu.InboundMessage, args []string, sessionKey string) error {
	if len(args) == 1 {
		return showClaudeWorkspacePermissionMenu(c.app, msg)
	}
	if len(args) != 2 {
		return fmt.Errorf("usage: /workspace permissions [MODE|inherit]")
	}
	_, _, ws := newWorkspaceConfigService(c.app).currentWorkspaceForMessage(msg)
	if ws == nil {
		return fmt.Errorf("workspace not found")
	}
	resp, err := newWorkspaceService(c.app).completeClaudeWorkspacePermissionModeSet(commandActionFromMessage(msg, nil), sessionKey, ws.ID, strings.TrimSpace(args[1]))
	if err != nil {
		return err
	}
	return replyCommandActionResponse(c.app, msg, resp)
}

func (c claudeBackendConfigurationFacade) appendWorkspaceSummaryLines(lines []string, currentWS *config.Workspace) []string {
	if currentWS == nil {
		return lines
	}
	effectiveMode := effectiveClaudePermissionMode(nil, currentWS, c.app.cfg.Claude)
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

func (claudeBackendConfigurationFacade) workspaceConfigButtons(sessionKey string) []feishu.Button {
	return []feishu.Button{{
		Text: submenuCommandLabel("默认权限", "/workspace permissions"),
		Type: "default",
		Value: map[string]any{
			"action":      "workspace.permission_mode.menu",
			"session_key": sessionKey,
		},
	}}
}

func (claudeBackendConfigurationFacade) workspaceSwitchInFlightNotice() string {
	return "。当前运行中的任务仍归属原会话；后续新任务会使用新工作区。"
}

func (claudeBackendConfigurationFacade) workspaceSwitchBindingFailureNotice() string {
	return "。自动绑定会话失败，可稍后重试。"
}

func (claudeBackendConfigurationFacade) workspaceSwitchBindingNotice(binding *workspaceThreadBinding) string {
	if binding != nil && binding.Resumed {
		return "。已自动恢复该工作区最近使用的会话。"
	}
	return "。已自动创建新会话。"
}

func (c claudeBackendConfigurationFacade) renderModelMenuCard(sessionKey string) map[string]any {
	return newBackendConfigurationService(c.app).renderClaudeModelMenuCard(sessionKey)
}

func (c claudeBackendConfigurationFacade) completeGlobalModelSet(action *feishu.CardAction, modelID string) (*callback.CardActionTriggerResponse, error) {
	return newModelConfigService(c.app).completeClaudeModelSet(action, modelID)
}

func (c claudeBackendConfigurationFacade) completeGlobalReasoningEffortSet(action *feishu.CardAction, reasoningEffort string) (*callback.CardActionTriggerResponse, error) {
	return newModelConfigService(c.app).completeClaudeEffortSet(action, reasoningEffort)
}

func (c claudeBackendConfigurationFacade) statusCardBody(sess *state.Session) string {
	return newBackendConfigurationService(c.app).renderClaudeStatusBody(sess)
}
