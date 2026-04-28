package backend

import (
	"fmt"
	"strings"

	appcore "feidex/internal/app/appcore"
	appsessionctx "feidex/internal/app/sessionctx"
	appthreadview "feidex/internal/app/threadview"
	appworkspace "feidex/internal/app/workspace"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type codexConversationDriver struct{}
type claudeConversationDriver struct{}
type codexPermissionDriver struct{}
type claudePermissionDriver struct{}

func (codexDriver) Kind() string { return appruntime.BackendCodex }
func (claudeDriver) Kind() string { return appruntime.BackendClaude }

func (codexDriver) Capabilities() CapabilitySet {
	return CapabilitySet{
		Kind: appruntime.BackendCodex,
		Conversation: ConversationCapabilities{
			Slash:        "/thread",
			Noun:         "线程",
			SummaryLabel: "thread",
		},
		Permissions: PermissionCapabilities{
			Scopes: []PermissionScope{PermissionScopeWorkspace, PermissionScopeConversation},
		},
	}
}

func (claudeDriver) Capabilities() CapabilitySet {
	return CapabilitySet{
		Kind: appruntime.BackendClaude,
		Conversation: ConversationCapabilities{
			Slash:        "/session",
			Noun:         "会话",
			SummaryLabel: "session",
		},
		Permissions: PermissionCapabilities{
			Scopes: []PermissionScope{PermissionScopeGlobal, PermissionScopeWorkspace, PermissionScopeConversation},
		},
	}
}

func (codexDriver) Runtime() RuntimeDriver {
	return backendRuntimeDriver{displayName: "Codex", autoRetry: "Codex 自动重试"}
}

func (claudeDriver) Runtime() RuntimeDriver {
	return backendRuntimeDriver{displayName: "Claude", autoRetry: "Claude 自动重试"}
}

func (codexDriver) Conversation() ConversationDriver { return codexConversationDriver{} }
func (claudeDriver) Conversation() ConversationDriver { return claudeConversationDriver{} }
func (codexDriver) Permission() PermissionDriver { return codexPermissionDriver{} }
func (claudeDriver) Permission() PermissionDriver { return claudePermissionDriver{} }

func (codexConversationDriver) PrimarySlash() string { return "/thread" }
func (claudeConversationDriver) PrimarySlash() string { return "/session" }

func (codexConversationDriver) Noun() string { return "线程" }
func (claudeConversationDriver) Noun() string { return "会话" }

func (codexConversationDriver) SummaryLabel() string { return "thread" }
func (claudeConversationDriver) SummaryLabel() string { return "session" }

func (codexConversationDriver) WorkspaceSwitchInFlightNotice() string {
	return "。当前运行中的任务仍归属原线程；后续新任务会使用新工作区。"
}

func (claudeConversationDriver) WorkspaceSwitchInFlightNotice() string {
	return "。当前运行中的任务仍归属原会话；后续新任务会使用新工作区。"
}

func (codexConversationDriver) WorkspaceSwitchBindingFailureNotice() string {
	return "。自动绑定 thread 失败，可稍后重试。"
}

func (claudeConversationDriver) WorkspaceSwitchBindingFailureNotice() string {
	return "。自动绑定会话失败，可稍后重试。"
}

func (codexConversationDriver) WorkspaceSwitchBindingNotice(binding *appworkspace.ThreadBinding) string {
	if binding != nil && binding.Resumed {
		return "。已自动恢复该工作区最近使用的线程。"
	}
	return "。已自动创建新线程。"
}

func (claudeConversationDriver) WorkspaceSwitchBindingNotice(binding *appworkspace.ThreadBinding) string {
	if binding != nil && binding.Resumed {
		return "。已自动恢复该工作区最近使用的会话。"
	}
	return "。已自动创建新会话。"
}

func (codexConversationDriver) EnsureWorkspaceThreadBinding(ops WorkspaceThreadOps, sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error) {
	return ops.EnsureCodexWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (claudeConversationDriver) EnsureWorkspaceThreadBinding(ops WorkspaceThreadOps, sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error) {
	return ops.EnsureClaudeWorkspaceThreadBinding(sessionKey, sess, ws)
}

func (codexConversationDriver) ListWorkspaceThreads(ops WorkspaceThreadOps, sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return ops.ListCodexWorkspaceThreads(sessionKey, ws, includeAll)
}

func (claudeConversationDriver) ListWorkspaceThreads(ops WorkspaceThreadOps, sessionKey string, ws *config.Workspace, includeAll bool) ([]codexrpc.ThreadListEntry, error) {
	return ops.ListClaudeWorkspaceThreads(sessionKey, ws, includeAll)
}

func (codexConversationDriver) StartWorkspaceThread(ops WorkspaceThreadOps, sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error) {
	return ops.StartCodexWorkspaceThread(sessionKey, sess, ws)
}

func (claudeConversationDriver) StartWorkspaceThread(ops WorkspaceThreadOps, sessionKey string, sess *state.Session, ws *config.Workspace) (*appworkspace.ThreadBinding, error) {
	return ops.StartClaudeWorkspaceThread(sessionKey, sess, ws)
}

func (codexPermissionDriver) SupportedScopes() []PermissionScope {
	return []PermissionScope{PermissionScopeWorkspace, PermissionScopeConversation}
}

func (claudePermissionDriver) SupportedScopes() []PermissionScope {
	return []PermissionScope{PermissionScopeGlobal, PermissionScopeWorkspace, PermissionScopeConversation}
}

func (codexPermissionDriver) WorkspaceCommandUsage() string {
	return appworkspace.CommandUsage
}

func (claudePermissionDriver) WorkspaceCommandUsage() string {
	return ClaudeWorkspaceCommandUsage
}

func (codexPermissionDriver) AppendWorkspaceSummaryLines(_ PermissionApp, lines []string, currentWS *config.Workspace) []string {
	if currentWS == nil {
		return lines
	}
	return append(lines,
		"默认 sandbox: `"+currentWS.SandboxMode+"`",
		"默认 policy: `"+currentWS.ApprovalPolicy+"`",
	)
}

func (claudePermissionDriver) AppendWorkspaceSummaryLines(app PermissionApp, lines []string, currentWS *config.Workspace) []string {
	if currentWS == nil || app == nil || app.Config() == nil {
		return lines
	}
	effectiveMode := effectiveClaudePermissionMode(nil, currentWS, app.Config().Claude)
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

func (codexPermissionDriver) WorkspaceConfigButtons(sessionKey string) []feishu.Button {
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

func (claudePermissionDriver) WorkspaceConfigButtons(sessionKey string) []feishu.Button {
	return []feishu.Button{{
		Text: submenuCommandLabel("默认权限", "/workspace permissions"),
		Type: "default",
		Value: map[string]any{
			"action":      "workspace.permission_mode.menu",
			"session_key": sessionKey,
		},
	}}
}

func (codexPermissionDriver) AppendStatusLines(_ PermissionApp, lines []string, sess *state.Session, ws *config.Workspace) []string {
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
	return append(lines,
		"workspace sandbox: `"+workspaceSandbox+"`",
		"workspace policy: `"+workspacePolicy+"`",
		"thread sandbox: "+threadSandbox,
		"thread policy: "+threadPolicy,
		"thread service tier: "+threadServiceTier,
		"生效 sandbox: `"+effectiveSandbox+"`",
		"生效 policy: `"+effectivePolicy+"`",
	)
}

func (claudePermissionDriver) AppendStatusLines(app PermissionApp, lines []string, sess *state.Session, ws *config.Workspace) []string {
	if app == nil || app.Config() == nil {
		return lines
	}
	workspacePermission := "-"
	sessionPermission := "跟随工作区"
	effectivePermission := "-"
	if ws != nil {
		workspacePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(nil, ws, app.Config().Claude))
		effectivePermission = claudePermissionModeLabel(effectiveClaudePermissionMode(sess, ws, app.Config().Claude))
	}
	if sess != nil && strings.TrimSpace(sess.ActiveClaudePermissionMode) != "" {
		sessionPermission = claudePermissionModeLabel(sess.ActiveClaudePermissionMode)
	}
	return append(lines,
		"workspace permission mode: "+workspacePermission,
		"session permission mode: "+sessionPermission,
		"effective permission mode: "+effectivePermission,
	)
}

func (d codexPermissionDriver) HandleWorkspaceCommand(req WorkspacePermissionCommandRequest) error {
	if len(req.Args) == 0 {
		return fmt.Errorf("usage: %s", d.WorkspaceCommandUsage())
	}
	switch strings.TrimSpace(req.Args[0]) {
	case "sandbox":
		if len(req.Args) == 1 {
			return req.ShowWorkspaceSandboxMenu(req.Message)
		}
		if len(req.Args) != 2 {
			return fmt.Errorf("usage: /workspace sandbox [MODE]")
		}
		_, _, ws := req.CurrentWorkspace(req.Message)
		if ws == nil {
			return fmt.Errorf("workspace not found")
		}
		resp, err := req.CompleteWorkspaceSandboxSet(req.CommandActionFromMessage(req.Message, nil), req.SessionKey, ws.ID, strings.TrimSpace(req.Args[1]))
		if err != nil {
			return err
		}
		return req.ReplyCommandActionResponse(req.Message, resp)
	case "policy":
		if len(req.Args) == 1 {
			return req.ShowWorkspacePolicyMenu(req.Message)
		}
		if len(req.Args) != 2 {
			return fmt.Errorf("usage: /workspace policy [POLICY]")
		}
		_, _, ws := req.CurrentWorkspace(req.Message)
		if ws == nil {
			return fmt.Errorf("workspace not found")
		}
		resp, err := req.CompleteWorkspacePolicySet(req.CommandActionFromMessage(req.Message, nil), req.SessionKey, ws.ID, strings.TrimSpace(req.Args[1]))
		if err != nil {
			return err
		}
		return req.ReplyCommandActionResponse(req.Message, resp)
	default:
		return fmt.Errorf("usage: %s", d.WorkspaceCommandUsage())
	}
}

func (d claudePermissionDriver) HandleWorkspaceCommand(req WorkspacePermissionCommandRequest) error {
	if len(req.Args) == 1 {
		return req.ShowWorkspacePermissionModeMenu(req.Message)
	}
	if len(req.Args) != 2 {
		return fmt.Errorf("usage: /workspace permissions [MODE|inherit]")
	}
	_, _, ws := req.CurrentWorkspace(req.Message)
	if ws == nil {
		return fmt.Errorf("workspace not found")
	}
	resp, err := req.CompleteWorkspacePermissionModeSet(req.CommandActionFromMessage(req.Message, nil), req.SessionKey, ws.ID, strings.TrimSpace(req.Args[1]))
	if err != nil {
		return err
	}
	return req.ReplyCommandActionResponse(req.Message, resp)
}

func (d codexPermissionDriver) HandleConversationCommand(req ConversationPermissionCommandRequest) error {
	if len(req.Args) == 0 {
		return fmt.Errorf("usage: /thread sandbox [MODE] | /thread policy [POLICY]")
	}
	switch strings.TrimSpace(req.Args[0]) {
	case "sandbox":
		if len(req.Args) == 1 {
			return req.ShowConversationSandboxMenu(req.Message)
		}
		if len(req.Args) != 2 {
			return fmt.Errorf("usage: /thread sandbox [MODE]")
		}
		_, _, _, threadID, err := req.CurrentThread(req.Message)
		if err != nil {
			return err
		}
		resp, err := req.CompleteConversationSandboxSet(req.CommandActionFromMessage(req.Message, nil), req.SessionKey, threadID, strings.TrimSpace(req.Args[1]))
		if err != nil {
			return err
		}
		return req.ReplyCommandActionResponse(req.Message, resp)
	case "policy":
		if len(req.Args) == 1 {
			return req.ShowConversationPolicyMenu(req.Message)
		}
		if len(req.Args) != 2 {
			return fmt.Errorf("usage: /thread policy [POLICY]")
		}
		_, _, _, threadID, err := req.CurrentThread(req.Message)
		if err != nil {
			return err
		}
		resp, err := req.CompleteConversationPolicySet(req.CommandActionFromMessage(req.Message, nil), req.SessionKey, threadID, strings.TrimSpace(req.Args[1]))
		if err != nil {
			return err
		}
		return req.ReplyCommandActionResponse(req.Message, resp)
	default:
		return fmt.Errorf("usage: /thread sandbox [MODE] | /thread policy [POLICY]")
	}
}

func (d claudePermissionDriver) HandleConversationCommand(req ConversationPermissionCommandRequest) error {
	if len(req.Args) == 1 {
		return req.ShowConversationPermissionModeMenu(req.Message)
	}
	if len(req.Args) != 2 {
		return fmt.Errorf("usage: /session permissions [MODE|inherit]")
	}
	_, _, _, threadID, err := req.CurrentThread(req.Message)
	if err != nil {
		return err
	}
	resp, err := req.CompleteConversationPermissionModeSet(req.CommandActionFromMessage(req.Message, nil), req.SessionKey, threadID, strings.TrimSpace(req.Args[1]))
	if err != nil {
		return err
	}
	return req.ReplyCommandActionResponse(req.Message, resp)
}

func (d codexPermissionDriver) RenderWorkspaceSandboxMenu(sessionKey string, deps WorkspacePermissionRenderDeps) (map[string]any, error) {
	if deps.App == nil {
		return nil, fmt.Errorf("app not configured")
	}
	_, ws, err := currentWorkspaceForDriver(deps.App, sessionKey)
	if err != nil {
		return nil, err
	}
	body := "配置当前工作区默认 sandbox。\n\n当前工作区: `" + ws.ID + "`\n当前值: `" + ws.SandboxMode + "`"
	buttons := make([]feishu.Button, 0, len(appworkspace.SandboxOptions())+1)
	for _, opt := range appworkspace.SandboxOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == ws.SandboxMode {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":       "workspace.sandbox.set",
				"session_key":  sessionKey,
				"workspace_id": ws.ID,
				"sandbox_mode": opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: commandLabel("返回工作区", "/workspace"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.workspace",
			"session_key": sessionKey,
		},
	})
	bodyText := body
	if deps.FormatMenuBody != nil {
		bodyText = deps.FormatMenuBody("workspace.sandbox.menu", body)
	}
	return deps.App.Feishu().SimpleStatusCard("配置 Sandbox", "blue", bodyText, buttons), nil
}

func (d codexPermissionDriver) RenderWorkspacePolicyMenu(sessionKey string, deps WorkspacePermissionRenderDeps) (map[string]any, error) {
	if deps.App == nil {
		return nil, fmt.Errorf("app not configured")
	}
	_, ws, err := currentWorkspaceForDriver(deps.App, sessionKey)
	if err != nil {
		return nil, err
	}
	body := "配置当前工作区默认 approval policy。\n\n当前工作区: `" + ws.ID + "`\n当前值: `" + ws.ApprovalPolicy + "`"
	buttons := make([]feishu.Button, 0, len(appworkspace.ApprovalPolicyOptions())+1)
	for _, opt := range appworkspace.ApprovalPolicyOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == ws.ApprovalPolicy {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":          "workspace.policy.set",
				"session_key":     sessionKey,
				"workspace_id":    ws.ID,
				"approval_policy": opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: commandLabel("返回工作区", "/workspace"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.workspace",
			"session_key": sessionKey,
		},
	})
	bodyText := body
	if deps.FormatMenuBody != nil {
		bodyText = deps.FormatMenuBody("workspace.policy.menu", body)
	}
	return deps.App.Feishu().SimpleStatusCard("配置 Policy", "blue", bodyText, buttons), nil
}

func (d claudePermissionDriver) RenderWorkspacePermissionModeMenu(sessionKey string, deps WorkspacePermissionRenderDeps) (map[string]any, error) {
	if deps.App == nil || deps.App.Config() == nil {
		return nil, fmt.Errorf("app not configured")
	}
	_, ws, err := currentWorkspaceForDriver(deps.App, sessionKey)
	if err != nil {
		return nil, err
	}
	effective := effectiveClaudePermissionMode(nil, ws, deps.App.Config().Claude)
	override := strings.TrimSpace(ws.ClaudePermissionMode)
	bodyLines := []string{
		"配置当前工作区默认 Claude 权限模式。",
		"",
		"当前工作区: `" + ws.ID + "`",
		"生效值: " + claudePermissionModeLabel(effective),
	}
	if override == "" {
		bodyLines = append(bodyLines, "当前覆盖: 跟随全局")
	} else {
		bodyLines = append(bodyLines, "当前覆盖: "+claudePermissionModeLabel(override))
	}
	buttons := make([]feishu.Button, 0, 6)
	followType := "default"
	followLabel := "跟随全局"
	if override == "" {
		followType = "primary"
		followLabel = "当前 · 跟随全局"
	}
	buttons = append(buttons, feishu.Button{
		Text: followLabel,
		Type: followType,
		Value: map[string]any{
			"action":       "workspace.permission_mode.set",
			"session_key":  sessionKey,
			"workspace_id": ws.ID,
			"mode":         "",
		},
	})
	for _, opt := range driverClaudePermissionModeOptions(driverClaudeBypassEnabled(deps.App.Config())) {
		btnType := "default"
		label := opt.Label
		if opt.Value == override {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":       "workspace.permission_mode.set",
				"session_key":  sessionKey,
				"workspace_id": ws.ID,
				"mode":         opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: commandLabel("返回工作区", "/workspace"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.workspace",
			"session_key": sessionKey,
		},
	})
	body := strings.Join(bodyLines, "\n")
	if deps.FormatMenuBody != nil {
		body = deps.FormatMenuBody("workspace.permission_mode.menu", body)
	}
	return deps.App.Feishu().SimpleStatusCard("配置默认权限", "blue", body, buttons), nil
}

func (d codexPermissionDriver) RenderWorkspacePermissionModeMenu(string, WorkspacePermissionRenderDeps) (map[string]any, error) {
	return nil, fmt.Errorf("当前 backend 不支持 /workspace permissions")
}

func (d claudePermissionDriver) RenderWorkspaceSandboxMenu(string, WorkspacePermissionRenderDeps) (map[string]any, error) {
	return nil, fmt.Errorf("当前 backend 不支持 /workspace sandbox")
}

func (d claudePermissionDriver) RenderWorkspacePolicyMenu(string, WorkspacePermissionRenderDeps) (map[string]any, error) {
	return nil, fmt.Errorf("当前 backend 不支持 /workspace policy")
}

func (d codexPermissionDriver) CompleteWorkspaceSandboxSet(sessionKey, workspaceID, sandboxMode string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range appworkspace.SandboxOptions() {
		if opt.Value == sandboxMode {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 sandbox"}}, nil
	}
	if _, err := deps.UpdateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.SandboxMode = sandboxMode
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := deps.RenderSandboxMenu(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 sandbox"},
		Card:  RawCard(card),
	}, nil
}

func (d codexPermissionDriver) CompleteWorkspacePolicySet(sessionKey, workspaceID, approvalPolicy string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range appworkspace.ApprovalPolicyOptions() {
		if opt.Value == approvalPolicy {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 policy"}}, nil
	}
	if _, err := deps.UpdateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.ApprovalPolicy = approvalPolicy
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := deps.RenderPolicyMenu(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 policy"},
		Card:  RawCard(card),
	}, nil
}

func (d claudePermissionDriver) CompleteWorkspacePermissionModeSet(sessionKey, workspaceID, rawMode string, deps WorkspacePermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	mode := ""
	warning := ""
	if override, ok := driverClaudePermissionOverrideValue(rawMode); ok {
		mode = override
	} else {
		var err error
		mode, warning, err = driverNormalizeRequestedClaudePermissionMode(deps.App.Config(), rawMode)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
	}
	if _, err := deps.UpdateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.ClaudePermissionMode = mode
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if deps.Session != nil && deps.App != nil && deps.App.Config() != nil {
		if sess := deps.Session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) == strings.TrimSpace(workspaceID) && strings.TrimSpace(sess.ActiveClaudePermissionMode) == "" {
			effective := effectiveClaudePermissionMode(sess, config.FindWorkspace(deps.App.Config(), workspaceID), deps.App.Config().Claude)
			if err := deps.ApplyRuntime(sessionKey, effective); err != nil {
				return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
			}
		}
	}
	card, err := deps.RenderPermissionMenu(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	content := "已更新 Claude 工作区权限模式"
	if warning != "" {
		content = warning
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: content},
		Card:  RawCard(card),
	}, nil
}

func (d claudePermissionDriver) CompleteWorkspaceSandboxSet(string, string, string, WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 backend 不支持 /workspace sandbox"}}, nil
}

func (d claudePermissionDriver) CompleteWorkspacePolicySet(string, string, string, WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 backend 不支持 /workspace policy"}}, nil
}

func (d codexPermissionDriver) CompleteWorkspacePermissionModeSet(string, string, string, WorkspacePermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 backend 不支持 /workspace permissions"}}, nil
}

func (d codexPermissionDriver) RenderConversationSandboxMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	if deps.App == nil || deps.Session == nil {
		return nil, fmt.Errorf("app not configured")
	}
	sess := deps.Session(sessionKey)
	workspaceID := appcore.DefaultWorkspaceID(deps.App)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(deps.App.Config(), workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	current := appsessionctx.EffectiveSandboxMode(sess, ws)
	body := "配置当前 thread 默认 sandbox。\n\nthread: `" + threadID + "`\n当前值: `" + current + "`"
	buttons := make([]feishu.Button, 0, len(appworkspace.SandboxOptions())+1)
	for _, opt := range appworkspace.SandboxOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == current {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":       "thread.sandbox.set",
				"session_key":  sessionKey,
				"thread_id":    threadID,
				"sandbox_mode": opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: deps.CommandLabel("返回 thread", "/thread"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.thread",
			"session_key": sessionKey,
		},
	})
	if deps.FormatMenuBody != nil {
		body = deps.FormatMenuBody("thread.sandbox.menu", body)
	}
	return deps.App.Feishu().SimpleStatusCard("配置 Thread Sandbox", "blue", body, buttons), nil
}

func (d codexPermissionDriver) RenderConversationPolicyMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	if deps.App == nil || deps.Session == nil {
		return nil, fmt.Errorf("app not configured")
	}
	sess := deps.Session(sessionKey)
	workspaceID := appcore.DefaultWorkspaceID(deps.App)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(deps.App.Config(), workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	current := appsessionctx.EffectiveApprovalPolicy(sess, ws)
	body := "配置当前 thread 默认 approval policy。\n\nthread: `" + threadID + "`\n当前值: `" + current + "`"
	buttons := make([]feishu.Button, 0, len(appworkspace.ApprovalPolicyOptions())+1)
	for _, opt := range appworkspace.ApprovalPolicyOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == current {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":          "thread.policy.set",
				"session_key":     sessionKey,
				"thread_id":       threadID,
				"approval_policy": opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: deps.CommandLabel("返回 thread", "/thread"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.thread",
			"session_key": sessionKey,
		},
	})
	if deps.FormatMenuBody != nil {
		body = deps.FormatMenuBody("thread.policy.menu", body)
	}
	return deps.App.Feishu().SimpleStatusCard("配置 Thread Policy", "blue", body, buttons), nil
}

func (d claudePermissionDriver) RenderConversationPermissionModeMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	if deps.App == nil || deps.Session == nil || deps.App.Config() == nil {
		return nil, fmt.Errorf("app not configured")
	}
	sess := deps.Session(sessionKey)
	workspaceID := appcore.DefaultWorkspaceID(deps.App)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(deps.App.Config(), workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动会话")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	effective := effectiveClaudePermissionMode(sess, ws, deps.App.Config().Claude)
	override := strings.TrimSpace(sess.ActiveClaudePermissionMode)
	bodyLines := []string{
		"配置当前 Claude 会话权限模式。",
		"",
		"session: `" + threadID + "`",
		"生效值: " + claudePermissionModeLabel(effective),
	}
	if override == "" {
		bodyLines = append(bodyLines, "当前覆盖: 跟随工作区")
	} else {
		bodyLines = append(bodyLines, "当前覆盖: "+claudePermissionModeLabel(override))
	}
	buttons := make([]feishu.Button, 0, 6)
	followType := "default"
	followLabel := "跟随工作区"
	if override == "" {
		followType = "primary"
		followLabel = "当前 · 跟随工作区"
	}
	buttons = append(buttons, feishu.Button{
		Text: followLabel,
		Type: followType,
		Value: map[string]any{
			"action":      "thread.permission_mode.set",
			"session_key": sessionKey,
			"thread_id":   threadID,
			"mode":        "",
		},
	})
	for _, opt := range driverClaudePermissionModeOptions(driverClaudeBypassEnabled(deps.App.Config())) {
		btnType := "default"
		label := opt.Label
		if opt.Value == override {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: map[string]any{
				"action":      "thread.permission_mode.set",
				"session_key": sessionKey,
				"thread_id":   threadID,
				"mode":        opt.Value,
			},
		})
	}
	buttons = append(buttons, feishu.Button{
		Text: deps.CommandLabel("返回会话", "/session"),
		Type: "default",
		Value: map[string]any{
			"action":      "menu.thread",
			"session_key": sessionKey,
		},
	})
	body := strings.Join(bodyLines, "\n")
	if deps.FormatMenuBody != nil {
		body = deps.FormatMenuBody("thread.permission_mode.menu", body)
	}
	return deps.App.Feishu().SimpleStatusCard("配置会话权限", "blue", body, buttons), nil
}

func (d claudePermissionDriver) RenderConversationSandboxMenu(string, ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, fmt.Errorf("当前 backend 不支持 /thread sandbox")
}

func (d claudePermissionDriver) RenderConversationPolicyMenu(string, ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, fmt.Errorf("当前 backend 不支持 /thread policy")
}

func (d codexPermissionDriver) RenderConversationPermissionModeMenu(string, ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, fmt.Errorf("当前 backend 不支持 /session permissions")
}

func (d codexPermissionDriver) CompleteConversationSandboxSet(sessionKey, threadID, sandboxMode string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range appworkspace.SandboxOptions() {
		if opt.Value == sandboxMode {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 sandbox"}}, nil
	}
	sess := deps.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadSandboxMode = sandboxMode
	if err := deps.SaveSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := deps.RenderSandboxMenu(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread sandbox"},
		Card:  RawCard(card),
	}, nil
}

func (d codexPermissionDriver) CompleteConversationPolicySet(sessionKey, threadID, approvalPolicy string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range appworkspace.ApprovalPolicyOptions() {
		if opt.Value == approvalPolicy {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 policy"}}, nil
	}
	sess := deps.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadApprovalPolicy = approvalPolicy
	if err := deps.SaveSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := deps.RenderPolicyMenu(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread policy"},
		Card:  RawCard(card),
	}, nil
}

func (d claudePermissionDriver) CompleteConversationPermissionModeSet(sessionKey, threadID, rawMode string, deps ConversationPermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	sess := deps.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前会话已失效"}}, nil
	}
	mode := ""
	warning := ""
	if override, ok := driverClaudePermissionOverrideValue(rawMode); ok {
		mode = override
	} else {
		var err error
		mode, warning, err = deps.NormalizeRequested(rawMode)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
	}
	sess.ActiveClaudePermissionMode = mode
	if err := deps.SaveSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if deps.App != nil && deps.App.Config() != nil {
		effective := effectiveClaudePermissionMode(sess, config.FindWorkspace(deps.App.Config(), sess.WorkspaceID), deps.App.Config().Claude)
		if err := deps.ApplyRuntime(sessionKey, effective); err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
		}
	}
	card, err := deps.RenderPermissionMenu(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	content := "已更新 Claude 会话权限模式"
	if warning != "" {
		content = warning
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: content},
		Card:  RawCard(card),
	}, nil
}

func (d claudePermissionDriver) CompleteConversationSandboxSet(string, string, string, ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 backend 不支持 /thread sandbox"}}, nil
}

func (d claudePermissionDriver) CompleteConversationPolicySet(string, string, string, ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 backend 不支持 /thread policy"}}, nil
}

func (d codexPermissionDriver) CompleteConversationPermissionModeSet(string, string, string, ConversationPermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 backend 不支持 /session permissions"}}, nil
}

func currentWorkspaceForDriver(app appcore.AppConfig, sessionKey string) (*state.Session, *config.Workspace, error) {
	if app == nil || app.Config() == nil {
		return nil, nil, fmt.Errorf("app not configured")
	}
	var sess *state.Session
	if store := app.Store(); store != nil {
		sess = store.GetSession(strings.TrimSpace(sessionKey))
	}
	workspaceID := appcore.DefaultWorkspaceID(app)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(app.Config(), workspaceID)
	if ws == nil {
		return sess, nil, fmt.Errorf("current workspace not found")
	}
	return sess, ws, nil
}

func driverClaudeBypassEnabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Claude.DangerouslySkipPermissions
}

func driverClaudePermissionModeOptions(includeBypass bool) []appruntime.ClaudePermissionModeOption {
	options := []appruntime.ClaudePermissionModeOption{
		{Value: string(appruntime.ClaudePermissionModeDefault), Label: "default"},
		{Value: string(appruntime.ClaudePermissionModeAcceptEdits), Label: "acceptEdits"},
	}
	if includeBypass {
		options = append(options, appruntime.ClaudePermissionModeOption{Value: string(appruntime.ClaudePermissionModeBypass), Label: "bypassPermissions"})
	}
	return options
}

func driverNormalizeRequestedClaudePermissionMode(cfg *config.Config, raw string) (string, string, error) {
	mode := normalizeClaudePermissionModeValue(raw)
	switch mode {
	case string(appruntime.ClaudePermissionModeDefault), string(appruntime.ClaudePermissionModeAcceptEdits), string(appruntime.ClaudePermissionModeBypass):
	default:
		return "", "", fmt.Errorf("不支持的 Claude 权限模式 `%s`", strings.TrimSpace(raw))
	}
	if mode == string(appruntime.ClaudePermissionModeBypass) && !driverClaudeBypassEnabled(cfg) {
		return "", "", fmt.Errorf("当前未启用 `claude.dangerously_skip_permissions`，不能切到 `bypassPermissions`")
	}
	return mode, "", nil
}

func driverClaudePermissionOverrideValue(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "", "inherit", "follow", "workspace", "global":
		return "", true
	default:
		return "", false
	}
}
