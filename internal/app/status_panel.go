package app

import (
	"context"
	"fmt"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) renderStatusCard(sessionKey string) map[string]any {
	var sess *state.Session
	if strings.TrimSpace(sessionKey) != "" {
		sess = a.appState().session(sessionKey)
	}
	buttons := []feishu.Button{
		{Text: commandLabel("刷新", "/status"), Type: "default", Value: map[string]any{"action": "menu.status", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("Status", "blue", menuCardBodyForBackend(a.configuredBackend(), "menu.status", a.statusCardBody(sess)), buttons)
}

func (a *App) statusCardBody(sess *state.Session) string {
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
	if a.isClaudeBackend() {
		model = firstNonEmpty(configuredClaudeModel(a.cfg), claudeDefaultModelAlias)
		effort = firstNonEmpty(configuredClaudeEffort(a.cfg), "(follow Claude default)")
	} else {
		if model == "" {
			model = "(follow app-server default)"
		}
		if effort == "" {
			effort = "(follow model default)"
		}
	}
	if a.isClaudeBackend() {
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

func (a *App) commandStatus(msg *feishu.InboundMessage) error {
	card := a.renderStatusCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}
