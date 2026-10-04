package feishuapp

import (
	applicationrouting "feidex/internal/application/routing"
	"feidex/internal/domain/routing"
	"feidex/internal/textutil"

	"context"
	"fmt"
	"path/filepath"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type bindingService struct {
	app      *App
	renderer bindingCardRenderer
}

// bindingCardRenderer is the presentation capability used by group binding
// commands. Keeping it on the service makes card construction an explicit
// consumer-owned port instead of reaching through the host App for Feishu.
type bindingCardRenderer interface {
	SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any
}

func BuildBindingCommands(a *App) bindingService {
	var renderer bindingCardRenderer
	if a != nil {
		renderer = a.feishu
	}
	return bindingService{app: a, renderer: renderer}
}

func (s bindingService) commandCurrentBotGroupConfig(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if strings.TrimSpace(msg.ChatType) != "group" {
		return fmt.Errorf("该工作区配置只能在群聊中使用；私聊仍用于配置当前 Bot 的默认能力")
	}
	binding, err := s.app.bindings.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
	if err != nil {
		return err
	}
	if len(args) == 0 || strings.EqualFold(args[0], "status") {
		card := s.renderBindingStatusCard(s.app.configView().makeSessionKey(msg), binding)
		return replyCardEffect(s.app, msg, card)
	}
	if err := ensureSessionModelConfigWritable(s.app, s.app.configView().makeSessionKey(msg)); err != nil && (strings.EqualFold(strings.TrimSpace(args[0]), "model") || strings.EqualFold(strings.TrimSpace(args[0]), "effort") || strings.EqualFold(strings.TrimSpace(args[0]), "plan") || strings.EqualFold(strings.TrimSpace(args[0]), "plan_effort") || strings.EqualFold(strings.TrimSpace(args[0]), "review") || strings.EqualFold(strings.TrimSpace(args[0]), "subagent") || strings.EqualFold(strings.TrimSpace(args[0]), "small")) {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "use":
		if len(args) != 2 {
			return fmt.Errorf("usage: /workspace use WORKSPACE_ID")
		}
		updated, err := s.activateBindingWorkspace(binding, args[1])
		if err != nil {
			return err
		}
		if err := s.replyBindingUpdated(msg, "已设置当前工作区 `"+updated.WorkspaceID+"`。"); err != nil {
			return err
		}
		return nil
	case "new":
		if len(args) < 3 {
			return fmt.Errorf("usage: /workspace new WORKSPACE_ID CWD")
		}
		workspaceID := strings.TrimSpace(args[1])
		cwd := strings.TrimSpace(strings.Join(args[2:], " "))
		result, err := s.app.bindings.GroupWorkspaces.New(binding, workspaceID, workspaceID, cwd)
		if err != nil {
			return err
		}
		updated := result.Effects.Binding
		if err := s.replyBindingUpdated(msg, "已创建并设置当前工作区 `"+updated.WorkspaceID+"`。"); err != nil {
			return err
		}
		return nil
	case "clone":
		if len(args) < 2 {
			return fmt.Errorf("usage: /workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]")
		}
		result, err := s.app.bindings.GroupWorkspaces.Clone(s.app.Context(), binding, args[1:])
		if err != nil {
			return err
		}
		updated, targetDir := result.Effects.Binding, result.TargetDir
		if err := s.replyBindingUpdated(msg, "已 clone 并设置当前工作区 `"+updated.WorkspaceID+"`。\n\ncwd: `"+targetDir+"`"); err != nil {
			return err
		}
		return nil
	case "primary":
		return s.commandPrimary(msg, args[1:])
	case "model":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model set MODEL_ID|default")
		}
		value := clearableArg(args[1])
		result, err := s.app.bindings.ScopedRoutingConfiguration.Set(applicationrouting.Scope{ChatType: msg.ChatType, ChatID: msg.ChatID}, routing.Model, value)
		updated := result.Binding
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已保存当前群内模型（下一轮启动前应用）: "+renderOptionalBacktick(updated.ModelOverride))
	case "effort":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model effort EFFORT|default")
		}
		value := clearableArg(args[1])
		result, err := s.app.bindings.ScopedRoutingConfiguration.Set(applicationrouting.Scope{ChatType: msg.ChatType, ChatID: msg.ChatID}, routing.Effort, value)
		updated := result.Binding
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已保存当前群内推理强度（下一轮启动前应用）: "+renderOptionalBacktick(updated.ReasoningEffortOverride))
	case "plan", "review", "subagent", "small":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model %s MODEL|default", args[0])
		}
		value := clearableArg(args[1])
		role := strings.ToLower(strings.TrimSpace(args[0]))
		_, err := s.app.bindings.ScopedRoutingConfiguration.Set(applicationrouting.Scope{ChatType: msg.ChatType, ChatID: msg.ChatID}, routing.Setting(role), value)
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新当前群内"+role+" model（待对应会话边界生效）: "+renderOptionalBacktick(value))
	case "subagent_effort":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model subagent effort EFFORT|default")
		}
		value := clearableArg(args[1])
		result, err := s.app.bindings.ScopedRoutingConfiguration.Set(applicationrouting.Scope{ChatType: msg.ChatType, ChatID: msg.ChatID}, routing.SubagentEffort, value)
		updated := result.Binding
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新当前群内 subagent reasoning effort: "+renderOptionalBacktick(updated.SubagentReasoningEffortOverride))
	case "plan_effort":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model plan effort EFFORT|default")
		}
		value := clearableArg(args[1])
		result, err := s.app.bindings.ScopedRoutingConfiguration.Set(applicationrouting.Scope{ChatType: msg.ChatType, ChatID: msg.ChatID}, routing.PlanEffort, value)
		updated := result.Binding
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新当前群内 Plan reasoning effort: "+renderOptionalBacktick(updated.PlanReasoningEffortOverride))
	case "fast":
		if len(args) != 2 {
			return fmt.Errorf("usage: /fast fast|default|off")
		}
		value := clearableArg(args[1])
		if strings.EqualFold(strings.TrimSpace(args[1]), "off") {
			value = ""
		}
		if value != "" {
			value = applicationrouting.NormalizeServiceTier(value)
			if value == "" {
				return fmt.Errorf("unsupported service tier %q", args[1])
			}
		}
		result, err := s.app.bindings.ScopedRoutingConfiguration.Set(applicationrouting.Scope{ChatType: msg.ChatType, ChatID: msg.ChatID}, routing.ServiceTier, value)
		updated := result.Binding
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新当前群内响应速度: "+renderOptionalBacktick(updated.ServiceTierOverride))
	case "sandbox":
		return s.updateSimpleOverride(msg, binding, args, routing.Sandbox)
	case "policy":
		return s.updateSimpleOverride(msg, binding, args, routing.ApprovalPolicy)
	case "multiagent":
		return s.updateSimpleOverride(msg, binding, args, routing.MultiAgent)
	case "permissions", "permission":
		return s.updateSimpleOverride(msg, binding, args, routing.Permissions)
	default:
		return fmt.Errorf("usage: %s", currentBotCommandUsage)
	}
}

func (s bindingService) commandPrimary(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if strings.TrimSpace(msg.ChatType) != "group" {
		return fmt.Errorf("/primary 只能在群聊中使用")
	}
	_, initErr := ensureGroupPrimaryInitialized(context.Background(), s.app, msg.ChatType, msg.ChatID)
	if len(args) == 0 || strings.EqualFold(strings.TrimSpace(args[0]), "status") {
		body := "当前 Bot primary: `" + onOffLabel(isGroupPrimary(s.app, msg.ChatType, msg.ChatID)) + "`"
		if self := currentBotDisplayName(s.app); self != "" {
			body += "\n当前 Bot: `" + self + "`"
		}
		if initErr != nil && !hasGroupPrimaryState(s.app, msg.ChatType, msg.ChatID) {
			body += "\n\n自动读取群机器人数量失败: `" + initErr.Error() + "`"
		}
		return s.replyBindingUpdated(msg, body)
	}
	if len(args) != 1 || !strings.EqualFold(strings.TrimSpace(args[0]), "on") {
		return fmt.Errorf("usage: /primary on")
	}
	assignment, ok := groupPrimaryAssignmentForCommand(msg)
	if !ok || strings.TrimSpace(currentLiveBotOpenID(s.app)) != assignment.TargetBotOpenID {
		return fmt.Errorf("usage: /primary on（群内需要明确 @目标 Bot）")
	}
	return s.setPrimaryForMessage(msg)
}

func (s bindingService) setPrimaryForMessage(msg *feishu.InboundMessage) error {
	if msg == nil {
		return nil
	}
	currentOpenID := currentLiveBotOpenID(s.app)
	if currentOpenID == "" {
		return fmt.Errorf("bot open_id is required to set group primary")
	}
	updated, err := setGroupPrimaryState(s.app, msg.ChatType, msg.ChatID, true, msg)
	if err != nil {
		return err
	}
	if updated == nil {
		updated = groupPrimaryForChat(s.app, msg.ChatType, msg.ChatID)
	}
	body := "已更新 primary: `" + onOffLabel(isGroupPrimary(s.app, msg.ChatType, msg.ChatID)) + "`"
	if updated != nil {
		scheduleGroupAnnouncementStatusRefresh(s.app, updated.ChatID, "primary_updated")
	}
	return s.replyBindingUpdated(msg, body)
}

func (s bindingService) completeBindingUse(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return nil, nil
	}
	msg := commandMessageFromAction(s.app, action, sessionKey, "/workspace use")
	binding, err := s.app.bindings.RoutingConfiguration.EnsureBinding(msg.ChatType, msg.ChatID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	updated, err := s.activateBindingWorkspace(binding, workspaceID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已设置当前工作区 " + updated.WorkspaceID},
		Card:  rawCard(s.app.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(sessionKey)),
	}, nil
}

func (s bindingService) unbindGroupWorkspace(sessionKey string) error {
	if !groupBindingSessionScopeActive(s.app, sessionKey) {
		return fmt.Errorf("解除 workspace 绑定只能在群聊中使用")
	}
	return s.app.bindings.GroupWorkspaces.Unbind(sessionKey, bindingForSessionKey(s.app, sessionKey))
}

func (s bindingService) completeBindingWorkspaceUnbind(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if err := s.unbindGroupWorkspace(sessionKey); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已解除本群绑定，请重新选择工作区"},
		Card:  rawCard(s.app.bindings.WorkspacePresentation.RenderWorkspaceMenuCard(sessionKey)),
	}, nil
}

func (s bindingService) updateSimpleOverride(msg *feishu.InboundMessage, binding *state.AgentBinding, args []string, setting routing.Setting) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: /workspace %s VALUE|default", setting)
	}
	value := clearableArg(args[1])
	_, err := s.app.bindings.ScopedRoutingConfiguration.Set(applicationrouting.Scope{ChatType: msg.ChatType, ChatID: msg.ChatID}, setting, value)
	if err != nil {
		return err
	}
	return s.replyBindingUpdated(msg, "已更新当前群内 "+string(setting)+": "+renderOptionalBacktick(value))
}

func (s bindingService) activateBindingWorkspace(binding *state.AgentBinding, workspaceID string) (*state.AgentBinding, error) {
	return s.app.bindings.GroupWorkspaces.Select(binding, workspaceID)
}

func bindingWorkspaceForSessionKey(a *App, sessionKey string) *config.Workspace {
	binding := bindingForSessionKey(a, sessionKey)
	if binding == nil || strings.TrimSpace(binding.WorkspaceID) == "" {
		return nil
	}
	return config.FindWorkspace(a.cfg, binding.WorkspaceID)
}

func (s bindingService) replyBindingUpdated(msg *feishu.InboundMessage, body string) error {
	if msg == nil {
		return nil
	}
	card := s.renderBindingStatusCard(s.app.configView().makeSessionKey(msg), agentBindingForChat(s.app, msg.ChatType, msg.ChatID))
	if strings.TrimSpace(body) != "" {
		card = s.renderer.SimpleStatusCard("当前 Bot 群内配置", "green", strings.TrimSpace(body), nil)
	}
	return replyCardEffect(s.app, msg, card)
}

func (s bindingService) renderBindingStatusCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	chatType, chatID, _, _ := currentBotMenuContext(s.app, sessionKey)
	primaryLabel := onOffLabel(isGroupPrimary(s.app, chatType, chatID))
	if binding == nil {
		body := "当前 Bot 在本群还没有配置工作区。\nprimary: `" + primaryLabel + "`\n\n使用 `@Bot /workspace use WORKSPACE_ID` 选择已有工作区，也可以用 `@Bot /workspace new worktree` 基于当前 Git 仓库创建隔离 worktree，或用 `@Bot /workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]` 从仓库创建。"
		return s.renderer.SimpleStatusCard("工作区管理", "orange", menuCardBody("menu.workspace", body), []feishu.Button{groupBindingBackButton(sessionKey)})
	}
	statusLine := "状态: `工作区未配置`"
	workspaceLine := "workspace: `(未配置)`"
	if ws := config.FindWorkspace(s.app.cfg, binding.WorkspaceID); ws != nil {
		statusLine = "状态: `工作区已配置`"
		workspaceLine = "workspace: `" + ws.ID + "`\ncwd: `" + ws.Cwd + "`"
	} else if strings.TrimSpace(binding.WorkspaceID) != "" {
		statusLine = "状态: `工作区不可用`"
		workspaceLine = "workspace: `" + binding.WorkspaceID + "` (配置不存在)"
	}
	lines := []string{
		"frontend: `" + textutil.FirstNonEmpty(s.app.FrontendID(), "default") + "`",
		"backend: `" + textutil.FirstNonEmpty(s.app.configView().configuredBackend(), "unset") + "`",
		"chat: `" + binding.ChatType + "/" + binding.ChatID + "`",
		statusLine,
		"primary: `" + onOffLabel(isGroupPrimary(s.app, binding.ChatType, binding.ChatID)) + "`",
		workspaceLine,
		"model override: " + renderOptionalBacktick(binding.ModelOverride),
		"effort override: " + renderOptionalBacktick(binding.ReasoningEffortOverride),
		"service tier: " + renderOptionalBacktick(binding.ServiceTierOverride),
		"sandbox: " + renderOptionalBacktick(binding.SandboxModeOverride),
		"approval policy: " + renderOptionalBacktick(binding.ApprovalPolicyOverride),
		"multi-agent: " + renderOptionalBacktick(binding.MultiAgentModeOverride),
		"Claude permissions: " + renderOptionalBacktick(binding.ClaudePermissionMode),
		"\n常用命令：`/workspace use WORKSPACE_ID`、`/workspace new WORKSPACE_ID CWD`、`/workspace new worktree [BRANCH] [ID]`、`/workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]`、`@Bot /primary on`、`/model set MODEL|default`、`/model effort EFFORT|default`。",
	}
	if len(binding.PendingMessages) > 0 {
		preview := pendingBindingMessagePreview(binding.PendingMessages[0])
		lines = append(lines, fmt.Sprintf("\n已暂存原消息 pending queue `%d`，配置工作区后按顺序继续处理；下一条: `%s`", len(binding.PendingMessages), preview))
	} else if binding.PendingMessage != nil {
		preview := pendingBindingMessagePreview(binding.PendingMessage)
		lines = append(lines, "\n已暂存原消息，配置工作区后会继续处理: `"+preview+"`")
	}
	if !hasGroupPrimaryState(s.app, binding.ChatType, binding.ChatID) {
		lines = append(lines, "\n注意: 还没有完成本群 primary 判断；如果未 `@` 消息没有响应，请使用 `@Bot /primary on` 显式设置当前 Bot 为 primary。")
	} else if !isGroupPrimary(s.app, binding.ChatType, binding.ChatID) {
		lines = append(lines, "\n当前 Bot 不是本群 primary；未 `@` 的普通群消息不会由它处理。使用 `@Bot /primary on` 可切换。")
	}
	buttons := []feishu.Button{}
	if config.FindWorkspace(s.app.cfg, "default") != nil {
		buttons = append(buttons, feishu.Button{Text: "使用 default", Type: "default", Value: map[string]any{"action": "workspace.use.existing", "session_key": sessionKey, "workspace_id": "default"}})
	}
	buttons = append(buttons, feishu.Button{Text: "选择已有", Type: "default", Value: map[string]any{"action": "menu.workspace", "session_key": sessionKey}})
	buttons = append(buttons, groupBindingBackButton(sessionKey))
	color := "blue"
	if binding.Status != state.AgentBindingStatusActive.String() || strings.TrimSpace(binding.WorkspaceID) == "" {
		color = "orange"
	}
	return s.renderer.SimpleStatusCard("工作区管理", color, menuCardBody("menu.workspace", strings.Join(lines, "\n")), buttons)
}

func currentBotMenuContext(a *App, sessionKey string) (chatType, chatID, rootMessageID, userID string) {
	chatType, chatID, rootMessageID, userID = parseSessionKeyMeta(sessionKey)
	if chatType == "" || chatID == "" {
		inferredChatType, inferredChatID := sessionKeyChatForApp(a, sessionKey)
		chatType = textutil.FirstNonEmpty(chatType, inferredChatType)
		chatID = textutil.FirstNonEmpty(chatID, inferredChatID)
	}
	return chatType, chatID, rootMessageID, userID
}

func resolveConfigRelativePath(a *App, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	base := "."
	if a != nil && strings.TrimSpace(a.cfgPath) != "" {
		base = filepath.Dir(a.cfgPath)
	}
	return filepath.Clean(filepath.Join(base, value))
}

func clearableArg(value string) string {
	return routing.ClearableValue(value)
}

func onOffLabel(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func renderOptionalBacktick(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "`(default)`"
	}
	return "`" + value + "`"
}

const currentBotCommandUsage = "/workspace | /workspace use WORKSPACE_ID | /workspace new WORKSPACE_ID CWD | /workspace new worktree [BRANCH] [ID] | /workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR] | @Bot /primary on | /model set MODEL|default | /model effort EFFORT|default | /fast fast|default|off | /workspace sandbox MODE|default | /workspace policy POLICY|default | /workspace multiagent MODE|default | /workspace permissions MODE|default"
