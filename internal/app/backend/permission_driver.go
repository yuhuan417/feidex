package backend

import (
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"fmt"
	"strings"

	"feidex/internal/adapter/feishu/cardactions"
	appthreadview "feidex/internal/adapter/feishu/threadview"
	appcore "feidex/internal/app/appcore"
	appworkspace "feidex/internal/app/workspace"
	"feidex/internal/config"
	"feidex/internal/feishu"
	appruntime "feidex/internal/runtime"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type codexConversationDriver struct{}
type claudeConversationDriver struct{}
type codexPermissionDriver struct{}
type claudePermissionDriver struct{}

func (codexDriver) Kind() string  { return domainbackend.BackendCodex }
func (claudeDriver) Kind() string { return domainbackend.BackendClaude }

func (codexDriver) Capabilities() CapabilitySet {
	return CapabilitySet{
		Kind: domainbackend.BackendCodex,
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
		Kind: domainbackend.BackendClaude,
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

func (codexDriver) Conversation() ConversationDriver  { return codexConversationDriver{} }
func (claudeDriver) Conversation() ConversationDriver { return claudeConversationDriver{} }
func (codexDriver) Permission() PermissionDriver      { return codexPermissionDriver{} }
func (claudeDriver) Permission() PermissionDriver     { return claudePermissionDriver{} }

func (codexConversationDriver) PrimarySlash() string  { return "/thread" }
func (claudeConversationDriver) PrimarySlash() string { return "/session" }

func (codexConversationDriver) Noun() string  { return "线程" }
func (claudeConversationDriver) Noun() string { return "会话" }

func (codexConversationDriver) SummaryLabel() string  { return "thread" }
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

func (codexPermissionDriver) AppendStatusLines(_ PermissionDependencies, lines []string, sess *conversation.Session, ws *config.Workspace) []string {
	workspaceSandbox := "-"
	workspacePolicy := "-"
	workspaceMultiAgent := "-"
	effectiveSandbox := "-"
	effectivePolicy := "-"
	effectiveMultiAgent := "-"
	if ws != nil {
		workspaceSandbox = firstNonEmpty(ws.SandboxMode, "-")
		workspacePolicy = firstNonEmpty(ws.ApprovalPolicy, "-")
		workspaceMultiAgent = firstNonEmpty(ws.MultiAgentMode, "-")
		effectiveSandbox = conversation.EffectiveSandboxMode(sess, ws.SandboxMode)
		effectivePolicy = conversation.EffectiveApprovalPolicy(sess, ws.ApprovalPolicy)
		effectiveMultiAgent = conversation.EffectiveMultiAgentMode(sess, ws.MultiAgentMode)
	}
	threadSandbox := appthreadview.RenderThreadSettingValue("", "")
	threadPolicy := appthreadview.RenderThreadSettingValue("", "")
	threadMultiAgent := appthreadview.RenderThreadSettingValue("", "")
	threadServiceTier := "-"
	if sess != nil {
		threadSandbox = appthreadview.RenderThreadSettingValue(sess.ActiveThreadSandboxMode, "")
		threadPolicy = appthreadview.RenderThreadSettingValue(sess.ActiveThreadApprovalPolicy, "")
		threadMultiAgent = appthreadview.RenderThreadSettingValue(sess.ActiveThreadMultiAgentMode, "")
		threadServiceTier = appruntime.RenderServiceTierValue(sess.ActiveThreadServiceTier)
	}
	return append(lines,
		"workspace sandbox: `"+workspaceSandbox+"`",
		"workspace policy: `"+workspacePolicy+"`",
		"workspace multi-agent: `"+workspaceMultiAgent+"`",
		"thread sandbox: "+threadSandbox,
		"thread policy: "+threadPolicy,
		"thread multi-agent: "+threadMultiAgent,
		"thread service tier: "+threadServiceTier,
		"生效 sandbox: `"+effectiveSandbox+"`",
		"生效 policy: `"+effectivePolicy+"`",
		"生效 multi-agent: `"+effectiveMultiAgent+"`",
	)
}

func (claudePermissionDriver) AppendStatusLines(app PermissionDependencies, lines []string, sess *conversation.Session, ws *config.Workspace) []string {
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
	case "multiagent":
		if len(req.Args) == 1 {
			return req.ShowWorkspaceMultiAgentMenu(req.Message)
		}
		if len(req.Args) != 2 {
			return fmt.Errorf("usage: /workspace multiagent [MODE]")
		}
		_, _, ws := req.CurrentWorkspace(req.Message)
		if ws == nil {
			return fmt.Errorf("workspace not found")
		}
		resp, err := req.CompleteWorkspaceMultiAgentSet(req.CommandActionFromMessage(req.Message, nil), req.SessionKey, ws.ID, strings.TrimSpace(req.Args[1]))
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
	case "multiagent":
		if len(req.Args) == 1 {
			return req.ShowConversationMultiAgentMenu(req.Message)
		}
		if len(req.Args) != 2 {
			return fmt.Errorf("usage: /thread multiagent [MODE]")
		}
		_, _, _, threadID, err := req.CurrentThread(req.Message)
		if err != nil {
			return err
		}
		resp, err := req.CompleteConversationMultiAgentSet(req.CommandActionFromMessage(req.Message, nil), req.SessionKey, threadID, strings.TrimSpace(req.Args[1]))
		if err != nil {
			return err
		}
		return req.ReplyCommandActionResponse(req.Message, resp)
	default:
		return fmt.Errorf("usage: /thread sandbox [MODE] | /thread policy [POLICY] | /thread multiagent [MODE]")
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

func (d codexPermissionDriver) CompleteWorkspaceSandboxSet(sessionKey, workspaceID, sandboxMode string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	valid := strings.TrimSpace(sandboxMode) == ""
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
	valid := strings.TrimSpace(approvalPolicy) == ""
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

func (d codexPermissionDriver) CompleteWorkspaceMultiAgentSet(sessionKey, workspaceID, mode string, deps WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range appworkspace.MultiAgentModeOptions() {
		if opt.Value == mode {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持 multi-agent mode"}}, nil
	}
	if _, err := deps.UpdateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.MultiAgentMode = mode
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := deps.RenderMultiAgentMenu(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 multi-agent mode"},
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
		mode, warning, err = driverNormalizeRequestedClaudePermissionMode(deps.Permissions.Config(), rawMode)
		if err != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
	}
	if _, err := deps.UpdateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.ClaudePermissionMode = mode
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if deps.Session != nil && deps.Permissions != nil && deps.Permissions.Config() != nil {
		if sess := deps.Session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) == strings.TrimSpace(workspaceID) && strings.TrimSpace(sess.ActiveClaudePermissionMode) == "" {
			effective := effectiveClaudePermissionMode(sess, config.FindWorkspace(deps.Permissions.Config(), workspaceID), deps.Permissions.Config().Claude)
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

func (d claudePermissionDriver) CompleteWorkspaceMultiAgentSet(string, string, string, WorkspacePermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 backend 不支持 /workspace multiagent"}}, nil
}

func (d codexPermissionDriver) CompleteWorkspacePermissionModeSet(string, string, string, WorkspacePermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 backend 不支持 /workspace permissions"}}, nil
}

func (d codexPermissionDriver) RenderConversationSandboxMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	if deps.Permissions == nil || deps.Session == nil {
		return nil, fmt.Errorf("app not configured")
	}
	sess := deps.Session(sessionKey)
	workspaceID := appcore.DefaultWorkspaceIDFromConfig(deps.Permissions.Config())
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(deps.Permissions.Config(), workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.SandboxMode
	}
	current := conversation.EffectiveSandboxMode(sess, workspaceValue)
	workspaceDefault := "-"
	if ws != nil {
		workspaceDefault = firstNonEmpty(ws.SandboxMode, "-")
	}
	override := appthreadview.RenderThreadSettingValue(sess.ActiveThreadSandboxMode, "")
	body := "配置当前 thread 默认 sandbox。\n\nthread: `" + threadID + "`\n当前值: `" + current + "`\nworkspace 默认: `" + workspaceDefault + "`\n当前覆盖: " + override + "\n生效值: `" + current + "`"
	buttons := make([]feishu.Button, 0, len(appworkspace.SandboxOptions())+2)
	followType, followLabel := "default", "跟随 workspace"
	if strings.TrimSpace(sess.ActiveThreadSandboxMode) == "" {
		followType, followLabel = "primary", "当前 · 跟随 workspace"
	}
	buttons = append(buttons, feishu.Button{Text: followLabel, Type: followType, Value: cardactions.ThreadActionValue{Action: "thread.sandbox.set", SessionKey: sessionKey, ThreadID: threadID}.Map()})
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
			Value: cardactions.ThreadActionValue{
				Action:      "thread.sandbox.set",
				SessionKey:  sessionKey,
				ThreadID:    threadID,
				SandboxMode: opt.Value,
			}.Map(),
		})
	}
	buttons = append(buttons, feishu.Button{
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Value: cardactions.MenuActionValue{Action: "menu.thread", SessionKey: sessionKey}.Map(),
	})
	if deps.FormatMenuBody != nil {
		body = deps.FormatMenuBody("thread.sandbox.menu", body)
	}
	return feishu.SimpleStatusCard("配置 Thread Sandbox", "blue", body, buttons), nil
}

func (d codexPermissionDriver) RenderConversationPolicyMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	if deps.Permissions == nil || deps.Session == nil {
		return nil, fmt.Errorf("app not configured")
	}
	sess := deps.Session(sessionKey)
	workspaceID := appcore.DefaultWorkspaceIDFromConfig(deps.Permissions.Config())
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(deps.Permissions.Config(), workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.ApprovalPolicy
	}
	current := conversation.EffectiveApprovalPolicy(sess, workspaceValue)
	workspaceDefault := "-"
	if ws != nil {
		workspaceDefault = firstNonEmpty(ws.ApprovalPolicy, "-")
	}
	override := appthreadview.RenderThreadSettingValue(sess.ActiveThreadApprovalPolicy, "")
	body := "配置当前 thread 默认 approval policy。\n\nthread: `" + threadID + "`\n当前值: `" + current + "`\nworkspace 默认: `" + workspaceDefault + "`\n当前覆盖: " + override + "\n生效值: `" + current + "`"
	buttons := make([]feishu.Button, 0, len(appworkspace.ApprovalPolicyOptions())+2)
	followType, followLabel := "default", "跟随 workspace"
	if strings.TrimSpace(sess.ActiveThreadApprovalPolicy) == "" {
		followType, followLabel = "primary", "当前 · 跟随 workspace"
	}
	buttons = append(buttons, feishu.Button{Text: followLabel, Type: followType, Value: cardactions.ThreadActionValue{Action: "thread.policy.set", SessionKey: sessionKey, ThreadID: threadID}.Map()})
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
			Value: cardactions.ThreadActionValue{
				Action:         "thread.policy.set",
				SessionKey:     sessionKey,
				ThreadID:       threadID,
				ApprovalPolicy: opt.Value,
			}.Map(),
		})
	}
	buttons = append(buttons, feishu.Button{
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Value: cardactions.MenuActionValue{Action: "menu.thread", SessionKey: sessionKey}.Map(),
	})
	if deps.FormatMenuBody != nil {
		body = deps.FormatMenuBody("thread.policy.menu", body)
	}
	return feishu.SimpleStatusCard("配置 Thread Policy", "blue", body, buttons), nil
}

func (d codexPermissionDriver) RenderConversationMultiAgentMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	if deps.Permissions == nil || deps.Session == nil {
		return nil, fmt.Errorf("app not configured")
	}
	sess := deps.Session(sessionKey)
	workspaceID := appcore.DefaultWorkspaceIDFromConfig(deps.Permissions.Config())
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(deps.Permissions.Config(), workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动线程")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	workspaceValue := ""
	if ws != nil {
		workspaceValue = ws.MultiAgentMode
	}
	current := conversation.EffectiveMultiAgentMode(sess, workspaceValue)
	body := "配置当前 thread 默认 multi-agent mode。\n\nthread: `" + threadID + "`\n当前值: `" + current + "`"
	buttons := make([]feishu.Button, 0, len(appworkspace.MultiAgentModeOptions())+1)
	for _, opt := range appworkspace.MultiAgentModeOptions() {
		btnType := "default"
		label := opt.Label
		if opt.Value == current {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: cardactions.ThreadActionValue{
				Action:         "thread.multiagent.set",
				SessionKey:     sessionKey,
				ThreadID:       threadID,
				MultiAgentMode: opt.Value,
			}.Map(),
		})
	}
	buttons = append(buttons, feishu.Button{
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Value: cardactions.MenuActionValue{Action: "menu.thread", SessionKey: sessionKey}.Map(),
	})
	if deps.FormatMenuBody != nil {
		body = deps.FormatMenuBody("thread.multiagent.menu", body)
	}
	return feishu.SimpleStatusCard("配置 Thread Multi-Agent Mode", "blue", body, buttons), nil
}

func (d claudePermissionDriver) RenderConversationPermissionModeMenu(sessionKey string, deps ConversationPermissionRenderDeps) (map[string]any, error) {
	if deps.Permissions == nil || deps.Session == nil || deps.Permissions.Config() == nil {
		return nil, fmt.Errorf("app not configured")
	}
	sess := deps.Session(sessionKey)
	workspaceID := appcore.DefaultWorkspaceIDFromConfig(deps.Permissions.Config())
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(deps.Permissions.Config(), workspaceID)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return nil, fmt.Errorf("当前没有活动会话")
	}
	threadID := strings.TrimSpace(sess.ActiveThreadID)
	effective := effectiveClaudePermissionMode(sess, ws, deps.Permissions.Config().Claude)
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
		Value: cardactions.ThreadActionValue{
			Action:     "thread.permission_mode.set",
			SessionKey: sessionKey,
			ThreadID:   threadID,
		}.Map(),
	})
	for _, opt := range driverClaudePermissionModeOptions(driverClaudeBypassEnabled(deps.Permissions.Config())) {
		btnType := "default"
		label := opt.Label
		if opt.Value == override {
			btnType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{
			Text: label,
			Type: btnType,
			Value: cardactions.ThreadActionValue{
				Action:     "thread.permission_mode.set",
				SessionKey: sessionKey,
				ThreadID:   threadID,
				Mode:       opt.Value,
			}.Map(),
		})
	}
	buttons = append(buttons, feishu.Button{
		Text:  feishu.MenuBackButtonText,
		Type:  "default",
		Value: cardactions.MenuActionValue{Action: "menu.thread", SessionKey: sessionKey}.Map(),
	})
	body := strings.Join(bodyLines, "\n")
	if deps.FormatMenuBody != nil {
		body = deps.FormatMenuBody("thread.permission_mode.menu", body)
	}
	return feishu.SimpleStatusCard("配置会话权限", "blue", body, buttons), nil
}

func (d claudePermissionDriver) RenderConversationSandboxMenu(string, ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, fmt.Errorf("当前 backend 不支持 /thread sandbox")
}

func (d claudePermissionDriver) RenderConversationPolicyMenu(string, ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, fmt.Errorf("当前 backend 不支持 /thread policy")
}

func (d claudePermissionDriver) RenderConversationMultiAgentMenu(string, ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, fmt.Errorf("当前 backend 不支持 /thread multiagent")
}

func (d codexPermissionDriver) RenderConversationPermissionModeMenu(string, ConversationPermissionRenderDeps) (map[string]any, error) {
	return nil, fmt.Errorf("当前 backend 不支持 /session permissions")
}

func (d codexPermissionDriver) CompleteConversationSandboxSet(sessionKey, threadID, sandboxMode string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	valid := strings.TrimSpace(sandboxMode) == ""
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
	valid := strings.TrimSpace(approvalPolicy) == ""
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

func (d codexPermissionDriver) CompleteConversationMultiAgentSet(sessionKey, threadID, mode string, deps ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range appworkspace.MultiAgentModeOptions() {
		if opt.Value == mode {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持 multi-agent mode"}}, nil
	}
	sess := deps.Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(threadID) {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 thread 已失效"}}, nil
	}
	sess.ActiveThreadMultiAgentMode = mode
	if err := deps.SaveSession(sess); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, err := deps.RenderMultiAgentMenu(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 thread multi-agent mode"},
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
	if deps.Permissions != nil && deps.Permissions.Config() != nil {
		effective := effectiveClaudePermissionMode(sess, config.FindWorkspace(deps.Permissions.Config(), sess.WorkspaceID), deps.Permissions.Config().Claude)
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

func (d claudePermissionDriver) CompleteConversationMultiAgentSet(string, string, string, ConversationPermissionUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 backend 不支持 /thread multiagent"}}, nil
}

func (d codexPermissionDriver) CompleteConversationPermissionModeSet(string, string, string, ConversationPermissionModeUpdateDeps) (*callback.CardActionTriggerResponse, error) {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "当前 backend 不支持 /session permissions"}}, nil
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
