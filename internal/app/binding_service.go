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

func (s bindingService) commandBind(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if strings.TrimSpace(msg.ChatType) != "group" {
		return fmt.Errorf("/bind 只能在群聊中使用；私聊仍用于配置当前 bot 的默认能力")
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
			return fmt.Errorf("usage: /bind use WORKSPACE_ID")
		}
		updated, err := s.activateBindingWorkspace(binding, args[1])
		if err != nil {
			return err
		}
		if err := s.replyBindingUpdated(msg, "已绑定本机 workspace `"+updated.WorkspaceID+"`。"); err != nil {
			return err
		}
		return s.replayPendingBindingMessage(updated)
	case "new":
		if len(args) < 3 {
			return fmt.Errorf("usage: /bind new WORKSPACE_ID CWD")
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
		if err := s.replyBindingUpdated(msg, "已创建并绑定本机 workspace `"+updated.WorkspaceID+"`。"); err != nil {
			return err
		}
		return s.replayPendingBindingMessage(updated)
	case "clone":
		if len(args) < 2 {
			return fmt.Errorf("usage: /bind clone GIT_URL [WORKSPACE_ID] [--parent DIR]")
		}
		workspaceID, targetDir, err := s.cloneLocalWorkspace(msg, args[1:])
		if err != nil {
			return err
		}
		updated, err := s.activateBindingWorkspace(binding, workspaceID)
		if err != nil {
			return err
		}
		if err := s.replyBindingUpdated(msg, "已 clone 并绑定本机 workspace `"+updated.WorkspaceID+"`。\n\ncwd: `"+targetDir+"`"); err != nil {
			return err
		}
		return s.replayPendingBindingMessage(updated)
	case "component":
		if len(args) != 2 {
			return fmt.Errorf("usage: /bind component NAME|default")
		}
		value := clearableArg(args[1])
		updated, err := s.updateBinding(binding, func(current *state.AgentBinding) {
			current.Component = strings.ToLower(value)
		})
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新 component: "+renderOptionalBacktick(updated.Component))
	case "primary":
		if len(args) != 2 {
			return fmt.Errorf("usage: /bind primary on|off")
		}
		primary, err := parseOnOff(args[1])
		if err != nil {
			return err
		}
		updated, err := s.updateBinding(binding, func(current *state.AgentBinding) {
			current.Primary = primary
		})
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新 primary: `"+onOffLabel(updated.Primary)+"`")
	case "model":
		if len(args) != 2 {
			return fmt.Errorf("usage: /bind model MODEL_ID|default")
		}
		value := clearableArg(args[1])
		updated, err := s.updateBinding(binding, func(current *state.AgentBinding) {
			current.ModelOverride = value
		})
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新 binding model: "+renderOptionalBacktick(updated.ModelOverride))
	case "effort":
		if len(args) != 2 {
			return fmt.Errorf("usage: /bind effort EFFORT|default")
		}
		value := clearableArg(args[1])
		updated, err := s.updateBinding(binding, func(current *state.AgentBinding) {
			current.ReasoningEffortOverride = value
		})
		if err != nil {
			return err
		}
		return s.replyBindingUpdated(msg, "已更新 binding effort: "+renderOptionalBacktick(updated.ReasoningEffortOverride))
	case "fast":
		if len(args) != 2 {
			return fmt.Errorf("usage: /bind fast fast|default|off")
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
		return s.replyBindingUpdated(msg, "已更新 binding fast: "+renderOptionalBacktick(updated.ServiceTierOverride))
	case "sandbox":
		return s.updateSimpleOverride(msg, binding, args, "sandbox", func(current *state.AgentBinding, value string) { current.SandboxModeOverride = value }, func(current *state.AgentBinding) string { return current.SandboxModeOverride })
	case "policy":
		return s.updateSimpleOverride(msg, binding, args, "policy", func(current *state.AgentBinding, value string) { current.ApprovalPolicyOverride = value }, func(current *state.AgentBinding) string { return current.ApprovalPolicyOverride })
	case "multiagent":
		return s.updateSimpleOverride(msg, binding, args, "multiagent", func(current *state.AgentBinding, value string) { current.MultiAgentModeOverride = value }, func(current *state.AgentBinding) string { return current.MultiAgentModeOverride })
	case "permissions", "permission":
		return s.updateSimpleOverride(msg, binding, args, "permissions", func(current *state.AgentBinding, value string) { current.ClaudePermissionMode = value }, func(current *state.AgentBinding) string { return current.ClaudePermissionMode })
	default:
		return fmt.Errorf("usage: %s", bindCommandUsage)
	}
}

func (s bindingService) completeMenuBinding(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return nil, nil
	}
	msg := commandMessageFromAction(s.app, action, sessionKey, "/bind")
	binding, err := s.ensureBindingForMessage(msg)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开当前 Bot 配置"},
		Card:  rawCard(s.renderBindingStatusCard(sessionKey, binding)),
	}, nil
}

func (s bindingService) completeBindingUse(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return nil, nil
	}
	msg := commandMessageFromAction(s.app, action, sessionKey, "/bind")
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
		Toast: &callback.Toast{Type: "success", Content: "已绑定 workspace " + updated.WorkspaceID},
		Card:  rawCard(s.renderBindingStatusCard(sessionKey, updated)),
	}, nil
}

func (s bindingService) ensureBindingForMessage(msg *feishu.InboundMessage) (*state.AgentBinding, error) {
	if s.app == nil || msg == nil {
		return nil, fmt.Errorf("app not initialized")
	}
	chatType := strings.TrimSpace(msg.ChatType)
	chatID := strings.TrimSpace(msg.ChatID)
	if chatType != "group" || chatID == "" {
		return nil, fmt.Errorf("/bind 只能在群聊中使用")
	}
	if binding := agentBindingForChat(s.app, chatType, chatID); binding != nil {
		return binding, nil
	}
	binding := &state.AgentBinding{
		ID:         defaultBindingID(s.app.FrontendID(), chatType, chatID),
		FrontendID: s.app.FrontendID(),
		ChatID:     chatID,
		ChatType:   chatType,
		Primary:    !hasAnyPrimaryBindingForChat(s.app, chatType, chatID),
		Status:     state.AgentBindingStatusPending.String(),
	}
	if err := s.app.State().SaveAgentBinding(binding); err != nil {
		return nil, err
	}
	return s.app.State().AgentBinding(binding.ID), nil
}

func (s bindingService) updateSimpleOverride(msg *feishu.InboundMessage, binding *state.AgentBinding, args []string, name string, set func(*state.AgentBinding, string), get func(*state.AgentBinding) string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: /bind %s VALUE|default", name)
	}
	value := clearableArg(args[1])
	updated, err := s.updateBinding(binding, func(current *state.AgentBinding) { set(current, value) })
	if err != nil {
		return err
	}
	return s.replyBindingUpdated(msg, "已更新 binding "+name+": "+renderOptionalBacktick(get(updated)))
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
		if !current.Primary && !hasAnyPrimaryBindingForChatExcluding(s.app, current.ChatType, current.ChatID, current.ID) {
			current.Primary = true
		}
		current.Status = state.AgentBindingStatusActive.String()
	})
}

func (s bindingService) updateBinding(binding *state.AgentBinding, mutate func(*state.AgentBinding)) (*state.AgentBinding, error) {
	if binding == nil {
		return nil, fmt.Errorf("binding not initialized")
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
	if current.Primary {
		if err := clearOtherPrimaryBindingsForChat(s.app, current.ChatType, current.ChatID, current.ID); err != nil {
			return nil, err
		}
	}
	updated := s.app.State().AgentBinding(current.ID)
	if updated == nil {
		return nil, fmt.Errorf("binding %q not found after update", current.ID)
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
		card = s.app.feishu.SimpleStatusCard("当前 Bot 配置", "green", strings.TrimSpace(body), nil)
	}
	_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}

func (s bindingService) renderBindingStatusCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	if binding == nil {
		return s.app.feishu.SimpleStatusCard("当前 Bot 配置", "orange", "当前群还没有本机 binding。\n\n使用 `@Bot /bind use WORKSPACE_ID` 绑定已有 workspace。", nil)
	}
	workspaceLine := "workspace: `(未绑定)`"
	if ws := config.FindWorkspace(s.app.cfg, binding.WorkspaceID); ws != nil {
		workspaceLine = "workspace: `" + ws.ID + "`\ncwd: `" + ws.Cwd + "`"
	} else if strings.TrimSpace(binding.WorkspaceID) != "" {
		workspaceLine = "workspace: `" + binding.WorkspaceID + "` (配置不存在)"
	}
	lines := []string{
		"frontend: `" + firstNonEmpty(s.app.FrontendID(), "default") + "`",
		"backend: `" + firstNonEmpty(configuredBackend(s.app), "unset") + "`",
		"chat: `" + binding.ChatType + "/" + binding.ChatID + "`",
		"status: `" + binding.Status + "`",
		"primary: `" + onOffLabel(binding.Primary) + "`",
		"component: " + renderOptionalBacktick(binding.Component),
		workspaceLine,
		"model override: " + renderOptionalBacktick(binding.ModelOverride),
		"effort override: " + renderOptionalBacktick(binding.ReasoningEffortOverride),
		"service tier: " + renderOptionalBacktick(binding.ServiceTierOverride),
		"sandbox: " + renderOptionalBacktick(binding.SandboxModeOverride),
		"approval policy: " + renderOptionalBacktick(binding.ApprovalPolicyOverride),
		"multi-agent: " + renderOptionalBacktick(binding.MultiAgentModeOverride),
		"Claude permissions: " + renderOptionalBacktick(binding.ClaudePermissionMode),
		"\n常用命令：`/bind use WORKSPACE_ID`、`/bind new WORKSPACE_ID CWD`、`/bind clone GIT_URL [WORKSPACE_ID] [--parent DIR]`、`/bind primary on|off`、`/bind model MODEL|default`、`/bind effort EFFORT|default`。",
	}
	if binding.PendingMessage != nil {
		preview := truncate(strings.TrimSpace(binding.PendingMessage.Text), 80)
		if preview == "" && len(binding.PendingMessage.Attachments) > 0 {
			preview = fmt.Sprintf("%d 个附件", len(binding.PendingMessage.Attachments))
		}
		if preview == "" {
			preview = binding.PendingMessage.MessageID
		}
		lines = append(lines, "\n已暂存原消息，绑定 workspace 成功后会继续处理: `"+preview+"`")
	}
	if !hasAnyPrimaryBindingForChat(s.app, binding.ChatType, binding.ChatID) {
		lines = append(lines, "\n注意: 本机状态里这个群还没有 primary bot；未 `@` 的普通群消息不会被任何本机 bot 接收。使用 `@Bot /bind primary on` 设置当前 bot 为 primary。")
	}
	buttons := []feishu.Button{}
	if config.FindWorkspace(s.app.cfg, "default") != nil {
		buttons = append(buttons, feishu.Button{Text: "绑定 default", Type: "default", Value: map[string]any{"action": "bind.use", "session_key": sessionKey, "workspace_id": "default"}})
	}
	buttons = append(buttons, feishu.Button{Text: "选择已有", Type: "default", Value: map[string]any{"action": "bind.choose", "session_key": sessionKey}})
	buttons = append(buttons, feishu.Button{Text: "返回当前 Bot", Type: "default", Value: map[string]any{"action": "menu.current_bot", "session_key": sessionKey}})
	color := "blue"
	if binding.Status != state.AgentBindingStatusActive.String() || strings.TrimSpace(binding.WorkspaceID) == "" {
		color = "orange"
	}
	return s.app.feishu.SimpleStatusCard("当前 Bot 配置", color, menuCardBody("menu.binding", strings.Join(lines, "\n")), buttons)
}

func (s bindingService) renderBindingWorkspaceChooseCard(sessionKey string, binding *state.AgentBinding) map[string]any {
	if binding == nil {
		return s.renderBindingStatusCard(sessionKey, binding)
	}
	lines := []string{
		"为当前 bot 在本群的 binding 选择本机已有 workspace。",
		"当前 binding: `" + binding.ID + "`",
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
		buttons = append(buttons, feishu.Button{Text: label, Type: buttonType, Value: map[string]any{"action": "bind.use", "session_key": sessionKey, "workspace_id": workspaceID}})
	}
	buttons = append(buttons, groupBindingBackButton(sessionKey))
	return s.app.feishu.SimpleStatusCard("选择 Binding Workspace", "blue", menuCardBody("menu.binding", strings.Join(lines, "\n")), buttons)
}

func hasAnyPrimaryBindingForChat(a *App, chatType, chatID string) bool {
	return hasAnyPrimaryBindingForChatExcluding(a, chatType, chatID, "")
}

func hasAnyPrimaryBindingForChatExcluding(a *App, chatType, chatID, excludeID string) bool {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	excludeID = strings.TrimSpace(excludeID)
	if a == nil || a.Store() == nil || chatID == "" {
		return false
	}
	for _, binding := range a.Store().AllAgentBindings() {
		if binding == nil || strings.TrimSpace(binding.ID) == excludeID {
			continue
		}
		if strings.ToLower(strings.TrimSpace(binding.ChatType)) != chatType || strings.TrimSpace(binding.ChatID) != chatID {
			continue
		}
		if binding.Primary {
			return true
		}
	}
	return false
}

func clearOtherPrimaryBindingsForChat(a *App, chatType, chatID, keepID string) error {
	chatType = strings.ToLower(strings.TrimSpace(chatType))
	chatID = strings.TrimSpace(chatID)
	keepID = strings.TrimSpace(keepID)
	if a == nil || a.Store() == nil || chatID == "" || keepID == "" {
		return nil
	}
	for _, binding := range a.Store().AllAgentBindings() {
		if binding == nil || strings.TrimSpace(binding.ID) == keepID || !binding.Primary {
			continue
		}
		if strings.ToLower(strings.TrimSpace(binding.ChatType)) != chatType || strings.TrimSpace(binding.ChatID) != chatID {
			continue
		}
		binding.Primary = false
		if err := a.Store().UpsertAgentBinding(binding); err != nil {
			return err
		}
	}
	return nil
}

func renderCurrentBotMenuCard(a *App, sessionKey string) map[string]any {
	spec, _ := menuGroupSpec("menu.current_bot")
	body := spec.Description
	if chatType, chatID, _, _ := currentBotMenuContext(sessionKey); chatType == "group" && chatID != "" {
		if binding := agentBindingForChat(a, chatType, chatID); binding != nil {
			body += "\n\n当前 binding: `" + binding.Status + "`"
			body += "\nprimary: `" + onOffLabel(binding.Primary) + "`"
			body += "\nworkspace: " + renderOptionalBacktick(binding.WorkspaceID)
			body += "\ncomponent: " + renderOptionalBacktick(binding.Component)
			if !hasAnyPrimaryBindingForChat(a, chatType, chatID) {
				body += "\n注意: 本机状态里这个群还没有 primary bot。"
			}
		}
	}
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, spec.Label), "blue", menuCardBodyForSession(a, sessionKey, spec.Action, body), renderGroupMenuButtons(configuredBackend(a), spec.Action, sessionKey))
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
		return false, fmt.Errorf("usage: /bind primary on|off")
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

const bindCommandUsage = "/bind | /bind status | /bind use WORKSPACE_ID | /bind new WORKSPACE_ID CWD | /bind clone GIT_URL [WORKSPACE_ID] [--parent DIR] | /bind component NAME|default | /bind primary on|off | /bind model MODEL|default | /bind effort EFFORT|default | /bind fast fast|default | /bind sandbox MODE|default | /bind policy POLICY|default | /bind multiagent MODE|default | /bind permissions MODE|default"
