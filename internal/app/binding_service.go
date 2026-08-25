package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	appworkspacecmd "feidex/internal/app/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type bindingService struct {
	app *App
}

func newBindingService(a *App) bindingService {
	return bindingService{app: a}
}

func (s bindingService) commandCurrentBotGroupConfig(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if strings.TrimSpace(msg.ChatType) != "group" {
		return fmt.Errorf("该工作区配置只能在群聊中使用；私聊仍用于配置当前 Bot 的默认能力")
	}
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return err
	}
	if len(args) == 0 || strings.EqualFold(args[0], "status") {
		card := s.renderBindingStatusCard(makeSessionKey(s.app, msg), binding)
		_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
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
		return s.replayPendingBindingMessage(updated)
	case "new":
		if len(args) < 3 {
			return fmt.Errorf("usage: /workspace new WORKSPACE_ID CWD")
		}
		workspaceID := strings.TrimSpace(args[1])
		cwd := strings.TrimSpace(strings.Join(args[2:], " "))
		if _, err := s.createLocalWorkspace(workspaceID, workspaceID, cwd); err != nil {
			return err
		}
		updated, err := s.activateBindingWorkspace(binding, workspaceID)
		if err != nil {
			return err
		}
		if err := s.replyBindingUpdated(msg, "已创建并设置当前工作区 `"+updated.WorkspaceID+"`。"); err != nil {
			return err
		}
		return s.replayPendingBindingMessage(updated)
	case "clone":
		if len(args) < 2 {
			return fmt.Errorf("usage: /workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]")
		}
		workspaceID, targetDir, err := s.cloneLocalWorkspace(msg, args[1:])
		if err != nil {
			return err
		}
		updated, err := s.activateBindingWorkspace(binding, workspaceID)
		if err != nil {
			return err
		}
		if err := s.replyBindingUpdated(msg, "已 clone 并设置当前工作区 `"+updated.WorkspaceID+"`。\n\ncwd: `"+targetDir+"`"); err != nil {
			return err
		}
		return s.replayPendingBindingMessage(updated)
	case "primary":
		return s.commandPrimary(msg, args[1:])
	case "model":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model set MODEL_ID|default")
		}
		value := clearableArg(args[1])
		updated, err := s.updateBinding(binding, func(current *state.AgentBinding) {
			current.ModelOverride = value
		})
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新当前群内模型: "+renderOptionalBacktick(updated.ModelOverride))
	case "effort":
		if len(args) != 2 {
			return fmt.Errorf("usage: /model effort EFFORT|default")
		}
		value := clearableArg(args[1])
		updated, err := s.updateBinding(binding, func(current *state.AgentBinding) {
			current.ReasoningEffortOverride = value
		})
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新当前群内推理强度: "+renderOptionalBacktick(updated.ReasoningEffortOverride))
	case "fast":
		if len(args) != 2 {
			return fmt.Errorf("usage: /fast fast|default|off")
		}
		value := clearableArg(args[1])
		if strings.EqualFold(strings.TrimSpace(args[1]), "off") {
			value = ""
		}
		if value != "" {
			value = normalizeServiceTier(value)
			if value == "" {
				return fmt.Errorf("unsupported service tier %q", args[1])
			}
		}
		updated, err := s.updateBinding(binding, func(current *state.AgentBinding) {
			current.ServiceTierOverride = value
		})
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新当前群内响应速度: "+renderOptionalBacktick(updated.ServiceTierOverride))
	case "sandbox":
		return s.updateSimpleOverride(msg, binding, args, "sandbox", func(current *state.AgentBinding, value string) { current.SandboxModeOverride = value }, func(current *state.AgentBinding) string { return current.SandboxModeOverride })
	case "policy":
		return s.updateSimpleOverride(msg, binding, args, "policy", func(current *state.AgentBinding, value string) { current.ApprovalPolicyOverride = value }, func(current *state.AgentBinding) string { return current.ApprovalPolicyOverride })
	case "multiagent":
		return s.updateSimpleOverride(msg, binding, args, "multiagent", func(current *state.AgentBinding, value string) { current.MultiAgentModeOverride = value }, func(current *state.AgentBinding) string { return current.MultiAgentModeOverride })
	case "permissions", "permission":
		return s.updateSimpleOverride(msg, binding, args, "permissions", func(current *state.AgentBinding, value string) { current.ClaudePermissionMode = value }, func(current *state.AgentBinding) string { return current.ClaudePermissionMode })
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
	ownerOpenID := groupPrimaryOwnerOpenID(s.app, msg.ChatType, msg.ChatID)
	if initErr != nil {
		ownerOpenID = groupPrimaryOwnerOpenID(s.app, msg.ChatType, msg.ChatID)
	}
	if len(args) == 0 || strings.EqualFold(strings.TrimSpace(args[0]), "status") {
		body := "当前 Bot primary: `" + onOffLabel(isGroupPrimary(s.app, msg.ChatType, msg.ChatID)) + "`"
		body += "\nowner bot open_id: `" + firstNonEmpty(ownerOpenID, "(未设置)") + "`"
		if self := currentBotOpenID(s.app); self != "" {
			body += "\n当前 Bot open_id: `" + self + "`"
		}
		if initErr != nil && !hasGroupPrimaryState(s.app, msg.ChatType, msg.ChatID) {
			body += "\n\n自动读取群机器人数量失败: `" + initErr.Error() + "`"
		}
		return s.replyBindingUpdated(msg, body)
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: /primary on|off")
	}
	primaryValue, err := parseOnOff(args[0])
	if err != nil {
		return fmt.Errorf("usage: /primary on|off")
	}
	targetOpenID := currentOrMentionedBotOpenID(s.app, msg)
	var updated *state.GroupPrimary
	if primaryValue {
		if targetOpenID == "" {
			return fmt.Errorf("bot open_id is required to set group primary")
		}
		updated, err = setGroupPrimaryOwner(s.app, msg.ChatType, msg.ChatID, targetOpenID)
	} else if ownerOpenID == "" || ownerOpenID == targetOpenID {
		updated, err = setGroupPrimaryOwner(s.app, msg.ChatType, msg.ChatID, "")
	} else {
		updated = groupPrimaryForChat(s.app, msg.ChatType, msg.ChatID)
	}
	if err != nil {
		return err
	}
	if updated == nil {
		updated = groupPrimaryForChat(s.app, msg.ChatType, msg.ChatID)
	}
	body := "已更新 primary: `" + onOffLabel(isGroupPrimary(s.app, msg.ChatType, msg.ChatID)) + "`"
	if updated != nil {
		body += "\nowner bot open_id: `" + firstNonEmpty(strings.TrimSpace(updated.OwnerBotOpenID), "(未设置)") + "`"
	}
	return s.replyBindingUpdated(msg, body)
}

func (s bindingService) completeMenuBinding(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return nil, nil
	}
	msg := commandMessageFromAction(s.app, action, sessionKey, "/workspace")
	if _, err := s.ensureBindingForMessage(msg); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开工作区管理"},
		Card:  rawCard(newWorkspaceRenderServiceInner(s.app).RenderWorkspaceMenuCard(sessionKey)),
	}, nil
}

func (s bindingService) completeBindingUse(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return nil, nil
	}
	msg := commandMessageFromAction(s.app, action, sessionKey, "/workspace use")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	updated, err := s.activateBindingWorkspace(binding, workspaceID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	s.replayPendingBindingMessageAsync(updated)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已设置当前工作区 " + updated.WorkspaceID},
		Card:  rawCard(newWorkspaceRenderServiceInner(s.app).RenderWorkspaceMenuCard(sessionKey)),
	}, nil
}

func (s bindingService) ensureBindingForMessage(msg *feishu.InboundMessage) (*state.AgentBinding, error) {
	if s.app == nil || msg == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	chatType := strings.TrimSpace(msg.ChatType)
	chatID := strings.TrimSpace(msg.ChatID)
	if chatType != "group" || chatID == "" {
		return nil, fmt.Errorf("该命令只能在群聊中使用")
	}
	if binding := agentBindingForChat(s.app, chatType, chatID); binding != nil {
		return binding, nil
	}
	binding := &state.AgentBinding{
		ID:         defaultBindingID(s.app.FrontendID(), chatType, chatID),
		FrontendID: s.app.FrontendID(),
		ChatID:     chatID,
		ChatType:   chatType,
		Status:     state.AgentBindingStatusPending.String(),
	}
	if err := s.app.State().SaveAgentBinding(binding); err != nil {
		return nil, err
	}
	return s.app.State().AgentBinding(binding.ID), nil
}

func (s bindingService) updateSimpleOverride(msg *feishu.InboundMessage, binding *state.AgentBinding, args []string, name string, set func(*state.AgentBinding, string), get func(*state.AgentBinding) string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: /workspace %s VALUE|default", name)
	}
	value := clearableArg(args[1])
	updated, err := s.updateBinding(binding, func(current *state.AgentBinding) { set(current, value) })
	if err != nil {
		return err
	}
	return s.replyBindingUpdated(msg, "已更新当前群内 "+name+": "+renderOptionalBacktick(get(updated)))
}

func (s bindingService) activateBindingWorkspace(binding *state.AgentBinding, workspaceID string) (*state.AgentBinding, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("请指定 workspace_id")
	}
	if config.FindWorkspace(s.app.cfg, workspaceID) == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	return s.updateBinding(binding, func(current *state.AgentBinding) {
		current.WorkspaceID = workspaceID
		current.Status = state.AgentBindingStatusActive.String()
	})
}

func (s bindingService) updateBinding(binding *state.AgentBinding, mutate func(*state.AgentBinding)) (*state.AgentBinding, error) {
	if binding == nil {
		return nil, fmt.Errorf("当前 Bot 工作区配置未初始化")
	}
	current := *binding
	if mutate != nil {
		mutate(&current)
	}
	if strings.TrimSpace(current.WorkspaceID) != "" {
		current.Status = state.AgentBindingStatusActive.String()
	}
	if err := s.app.State().SaveAgentBinding(&current); err != nil {
		return nil, err
	}
	updated := s.app.State().AgentBinding(current.ID)
	if updated == nil {
		return nil, fmt.Errorf("当前 Bot 工作区配置 %q 更新后未找到", current.ID)
	}
	return updated, nil
}

func (s bindingService) createLocalWorkspace(id, name, cwd string) (*config.Workspace, error) {
	id = strings.TrimSpace(id)
	cwd = strings.TrimSpace(cwd)
	if id == "" {
		return nil, fmt.Errorf("workspace_id is required")
	}
	if cwd == "" {
		return nil, fmt.Errorf("cwd is required")
	}
	absCWD := resolveConfigRelativePath(s.app, cwd)
	if err := os.MkdirAll(absCWD, 0o755); err != nil {
		return nil, err
	}
	s.app.configMu.Lock()
	defer s.app.configMu.Unlock()
	if config.FindWorkspace(s.app.cfg, id) != nil {
		return nil, fmt.Errorf("workspace %q 已存在", id)
	}
	s.app.cfg.Workspaces = append(s.app.cfg.Workspaces, config.Workspace{
		ID:             id,
		Name:           firstNonEmpty(strings.TrimSpace(name), id),
		Cwd:            absCWD,
		ApprovalPolicy: "on-request",
		SandboxMode:    "workspace-write",
		MultiAgentMode: "explicitRequestOnly",
	})
	if err := s.app.cfg.Normalize(filepath.Dir(s.app.cfgPath)); err != nil {
		return nil, err
	}
	if err := config.Save(s.app.cfgPath, s.app.cfg); err != nil {
		return nil, err
	}
	return config.FindWorkspace(s.app.cfg, id), nil
}

func (s bindingService) cloneLocalWorkspace(msg *feishu.InboundMessage, args []string) (string, string, error) {
	repoURL, workspaceID, parentDir, err := appworkspacecmd.ParseCloneArgs(args)
	if err != nil {
		return "", "", err
	}
	mgmt := newWorkspaceManagementServiceInner(s.app)
	if strings.TrimSpace(parentDir) == "" {
		parentDir = mgmt.DefaultWorkspaceCloneParent(config.FindWorkspace(s.app.cfg, defaultWorkspaceID(s.app)))
	}
	parentDir = resolveConfigRelativePath(s.app, parentDir)
	plan, err := mgmt.PrepareWorkspaceClone(repoURL, workspaceID, parentDir)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(plan.TargetDir), 0o755); err != nil {
		return "", "", err
	}
	if err := mgmt.GitClone(context.Background(), strings.TrimSpace(repoURL), plan.TargetDir, nil); err != nil {
		return "", "", err
	}
	if _, err := s.createLocalWorkspace(plan.WorkspaceID, plan.WorkspaceID, plan.TargetDir); err != nil {
		return "", "", err
	}
	_ = msg
	return plan.WorkspaceID, plan.TargetDir, nil
}

func (s bindingService) replyBindingUpdated(msg *feishu.InboundMessage, body string) error {
	if msg == nil {
		return nil
	}
	card := s.renderBindingStatusCard(makeSessionKey(s.app, msg), agentBindingForChat(s.app, msg.ChatType, msg.ChatID))
	if strings.TrimSpace(body) != "" {
		card = s.app.feishu.SimpleStatusCard("当前 Bot 群内配置", "green", strings.TrimSpace(body), nil)
	}
	_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}

func (s bindingService) renderBindingStatusCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	chatType, chatID, _, _ := currentBotMenuContext(sessionKey)
	primaryLabel := onOffLabel(isGroupPrimary(s.app, chatType, chatID))
	if binding == nil {
		body := "当前 Bot 在本群还没有配置工作区。\nprimary: `" + primaryLabel + "`\n\n使用 `@Bot /workspace use WORKSPACE_ID` 选择已有工作区，或使用 `@Bot /workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]` 从仓库创建。"
		return s.app.feishu.SimpleStatusCard("工作区管理", "orange", menuCardBody("menu.workspace", body), []feishu.Button{groupBindingBackButton(sessionKey)})
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
		"frontend: `" + firstNonEmpty(s.app.FrontendID(), "default") + "`",
		"backend: `" + firstNonEmpty(configuredBackend(s.app), "unset") + "`",
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
		"\n常用命令：`/workspace use WORKSPACE_ID`、`/workspace new WORKSPACE_ID CWD`、`/workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]`、`/primary on|off`、`/model set MODEL|default`、`/model effort EFFORT|default`。",
	}
	if binding.PendingMessage != nil {
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
	return s.app.feishu.SimpleStatusCard("工作区管理", color, menuCardBody("menu.workspace", strings.Join(lines, "\n")), buttons)
}

func (s bindingService) renderBindingWorkspaceChooseCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	if binding == nil {
		return s.renderBindingStatusCard(sessionKey, binding)
	}
	lines := []string{
		"为当前 Bot 在本群选择本机已有 workspace。",
		"当前 workspace: " + renderOptionalBacktick(binding.WorkspaceID),
		"",
		"如果这台机器还没有该项目目录，请使用 `@Bot /workspace new WORKSPACE_ID CWD` 或 `@Bot /workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR]`。",
	}
	buttons := make([]feishu.Button, 0, len(s.app.cfg.Workspaces)+1)
	for _, ws := range s.app.cfg.Workspaces {
		workspaceID := strings.TrimSpace(ws.ID)
		if workspaceID == "" {
			continue
		}
		buttonType := "default"
		label := workspaceID
		if workspaceID == strings.TrimSpace(binding.WorkspaceID) {
			buttonType = "primary"
			label = "当前 · " + label
		}
		buttons = append(buttons, feishu.Button{Text: label, Type: buttonType, Value: map[string]any{"action": "workspace.use.existing", "session_key": sessionKey, "workspace_id": workspaceID}})
	}
	buttons = append(buttons, groupBindingBackButton(sessionKey))
	return s.app.feishu.SimpleStatusCard("选择工作区", "blue", menuCardBody("menu.workspace", strings.Join(lines, "\n")), buttons)
}

func renderCurrentBotMenuCard(a *App, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.current_bot")
	body := spec.Description
	if chatType, chatID, _, _ := currentBotMenuContext(sessionKey); chatType == "group" && chatID != "" {
		if binding := agentBindingForChat(a, chatType, chatID); binding != nil {
			body += "\n\n工作区状态: `" + currentBotWorkspaceStatusLabel(a, binding) + "`"
			body += "\nprimary: `" + onOffLabel(isGroupPrimary(a, chatType, chatID)) + "`"
			body += "\nworkspace: " + renderOptionalBacktick(binding.WorkspaceID)
			if !hasGroupPrimaryState(a, chatType, chatID) {
				body += "\n注意: 还没有完成本群 primary 判断。"
			}
		} else {
			body += "\n\n工作区状态: `工作区未配置`"
			body += "\nprimary: `" + onOffLabel(isGroupPrimary(a, chatType, chatID)) + "`"
			body += "\n使用 `/workspace` 选择、创建或 clone 当前 Bot 在本群的工作区。"
		}
	}
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, spec.Label), "blue", menuCardBodyForSession(a, sessionKey, spec.Action, body), renderGroupMenuButtons(configuredBackend(a), spec.Action, sessionKey))
}

func currentBotWorkspaceStatusLabel(a *App, binding *state.AgentBinding) string {
	if binding == nil || strings.TrimSpace(binding.WorkspaceID) == "" {
		return "工作区未配置"
	}
	if config.FindWorkspace(a.cfg, binding.WorkspaceID) == nil {
		return "工作区不可用"
	}
	return "工作区已配置"
}

func currentBotMenuContext(sessionKey string) (chatType, chatID, rootMessageID, userID string) {
	return parseSessionKeyMeta(sessionKey)
}

func defaultBindingID(frontendID, chatType, chatID string) string {
	return "binding_" + sanitizeBindingIDPart(frontendID) + "_" + sanitizeBindingIDPart(chatType) + "_" + sanitizeBindingIDPart(chatID)
}

func sanitizeBindingIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
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
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "default", "inherit", "follow", "clear", "unset":
		return ""
	default:
		return value
	}
}

func parseOnOff(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	default:
		return false, fmt.Errorf("usage: /primary on|off")
	}
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

const currentBotCommandUsage = "/workspace | /workspace use WORKSPACE_ID | /workspace new WORKSPACE_ID CWD | /workspace clone GIT_URL [WORKSPACE_ID] [--parent DIR] | /primary on|off | /model set MODEL|default | /model effort EFFORT|default | /fast fast|default|off | /workspace sandbox MODE|default | /workspace policy POLICY|default | /workspace multiagent MODE|default | /workspace permissions MODE|default"
