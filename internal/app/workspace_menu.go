package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type workspaceSettingOption struct {
	Value string
	Label string
}

const workspaceCommandUsage = "/workspace | /workspace list | /workspace new | /workspace clone GIT_URL [ID] [--parent DIR] | /workspace use ID | /workspace delete [ID] | /workspace sandbox [MODE] | /workspace policy [POLICY]"

func parseWorkspaceCloneArgs(args []string) (repoURL, workspaceID, parentDir string, err error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) != "clone" {
		return "", "", "", fmt.Errorf("usage: %s", workspaceCommandUsage)
	}
	repoURL = strings.TrimSpace(args[1])
	if repoURL == "" {
		return "", "", "", fmt.Errorf("usage: %s", workspaceCommandUsage)
	}
	switch len(args) {
	case 2:
		return repoURL, "", "", nil
	case 3:
		if strings.TrimSpace(args[2]) == "--parent" {
			return "", "", "", fmt.Errorf("usage: %s", workspaceCommandUsage)
		}
		return repoURL, strings.TrimSpace(args[2]), "", nil
	case 4:
		if strings.TrimSpace(args[2]) != "--parent" || strings.TrimSpace(args[3]) == "" {
			return "", "", "", fmt.Errorf("usage: %s", workspaceCommandUsage)
		}
		return repoURL, "", strings.TrimSpace(args[3]), nil
	case 5:
		if strings.TrimSpace(args[2]) == "" || strings.TrimSpace(args[3]) != "--parent" || strings.TrimSpace(args[4]) == "" {
			return "", "", "", fmt.Errorf("usage: %s", workspaceCommandUsage)
		}
		return repoURL, strings.TrimSpace(args[2]), strings.TrimSpace(args[4]), nil
	default:
		return "", "", "", fmt.Errorf("usage: %s", workspaceCommandUsage)
	}
}

func (s workspaceService) commandWorkspace(msg *feishu.InboundMessage, args []string) error {
	if len(args) == 0 {
		return newWorkspaceConfigService(s.app).showWorkspaceMenu(msg)
	}
	sessionKey := makeSessionKey(s.app, msg)
	if args[0] == "list" {
		return newWorkspaceConfigService(s.app).showWorkspaceMenu(msg)
	}
	if args[0] == "new" {
		return newWorkspaceManagementService(s.app).beginWorkspaceNew(msg)
	}
	if len(args) >= 2 && args[0] == "clone" {
		repoURL, workspaceID, parentDir, err := parseWorkspaceCloneArgs(args)
		if err != nil {
			return err
		}
		if parentDir != "" {
			err = newWorkspaceManagementService(s.app).cloneWorkspaceAndSwitchInSelectedParent(msg, repoURL, workspaceID, parentDir)
		} else {
			err = newWorkspaceManagementService(s.app).cloneWorkspaceAndSwitch(msg, repoURL, workspaceID)
		}
		var existingDirErr *workspaceCloneExistingDirError
		if errors.As(err, &existingDirErr) {
			return newWorkspaceManagementService(s.app).beginWorkspaceNewWithPayload(msg, sessionKey, workspaceNewTakeoverPayloadWithNotice(existingDirErr.WorkspaceID, existingDirErr.TargetDir, workspaceNewTakeoverNotice(existingDirErr.TargetDir)))
		}
		var existingWorkspaceErr *workspaceCloneExistingWorkspaceError
		if errors.As(err, &existingWorkspaceErr) {
			return replyCommandActionResponse(s.app, msg, &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "info", Content: "目标目录已经由现有工作区接管，可直接切换"},
				Card:  rawCard(newWorkspaceRenderService(s.app).renderWorkspaceCloneSwitchExistingCard(sessionKey, existingWorkspaceErr.WorkspaceID, existingWorkspaceErr.TargetDir)),
			})
		}
		return err
	}
	if args[0] == "delete" {
		if len(args) == 1 {
			return newWorkspaceConfigService(s.app).showWorkspaceDeleteMenu(msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /workspace delete [ID]")
		}
		workspaceID := strings.TrimSpace(args[1])
		if err := newWorkspaceConfigService(s.app).deleteWorkspace(sessionKey, workspaceID); err != nil {
			return err
		}
		reply := "已删除工作区 " + workspaceID + "，仅移除配置，未删除目录"
		return s.app.feishu.ReplyText(context.Background(), msg.MessageID, reply, replyInThreadEnabled(s.app, msg.ChatType))
	}
	if args[0] == "permissions" {
		return newBackendConfigurationService(s.app).handleBackendWorkspacePermissionCommand(msg, args, sessionKey)
	}
	if args[0] == "sandbox" {
		if len(args) == 1 {
			return newWorkspaceConfigService(s.app).showWorkspaceSandboxMenu(msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /workspace sandbox [MODE]")
		}
		_, _, ws := newWorkspaceConfigService(s.app).currentWorkspaceForMessage(msg)
		if ws == nil {
			return fmt.Errorf("workspace not found")
		}
		resp, err := newWorkspaceService(s.app).completeWorkspaceSandboxSet(commandActionFromMessage(msg, nil), sessionKey, ws.ID, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return replyCommandActionResponse(s.app, msg, resp)
	}
	if args[0] == "policy" {
		if len(args) == 1 {
			return newWorkspaceConfigService(s.app).showWorkspacePolicyMenu(msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /workspace policy [POLICY]")
		}
		_, _, ws := newWorkspaceConfigService(s.app).currentWorkspaceForMessage(msg)
		if ws == nil {
			return fmt.Errorf("workspace not found")
		}
		resp, err := newWorkspaceService(s.app).completeWorkspacePolicySet(commandActionFromMessage(msg, nil), sessionKey, ws.ID, strings.TrimSpace(args[1]))
		if err != nil {
			return err
		}
		return replyCommandActionResponse(s.app, msg, resp)
	}
	if len(args) >= 2 && args[0] == "use" {
		appState := appState(s.app)
		ws := config.FindWorkspace(s.app.cfg, args[1])
		if ws == nil {
			return fmt.Errorf("workspace %q not found", args[1])
		}
		sess := appState.session(sessionKey)
		if sess == nil {
			sess = &state.Session{Key: sessionKey, ChatID: msg.ChatID, ChatType: msg.ChatType, OwnerUserID: msg.UserID}
		}
		switchSessionWorkspace(sess, ws.ID)
		if err := appState.saveSession(sess); err != nil {
			return err
		}
		reply := "已切换工作区到 " + ws.ID
		if sessionHasInFlightSubmission(sess) {
			reply += newBackendConfigurationService(s.app).backendWorkspaceSwitchInFlightNotice()
			return s.app.feishu.ReplyText(context.Background(), msg.MessageID, reply, replyInThreadEnabled(s.app, msg.ChatType))
		}
		binding, err := newWorkspaceThreadService(s.app).ensureWorkspaceThreadBinding(sessionKey, sess, ws)
		if err != nil {
			slog.Warn("workspace switch thread binding failed",
				"session_key", sessionKey,
				"workspace_id", ws.ID,
				"cwd", ws.Cwd,
				"error", err,
			)
			reply += newBackendConfigurationService(s.app).backendWorkspaceSwitchBindingFailureNotice()
			return s.app.feishu.ReplyText(context.Background(), msg.MessageID, reply, replyInThreadEnabled(s.app, msg.ChatType))
		}
		reply += newBackendConfigurationService(s.app).backendWorkspaceSwitchBindingNotice(binding)
		return s.app.feishu.ReplyText(context.Background(), msg.MessageID, reply, replyInThreadEnabled(s.app, msg.ChatType))
	}
	return fmt.Errorf("usage: %s", newBackendConfigurationService(s.app).backendWorkspaceCommandUsage())
}

func (s workspaceConfigService) showWorkspaceMenu(msg *feishu.InboundMessage) error {
	card := newWorkspaceConfigService(s.app).renderWorkspaceMenuCard(makeSessionKey(s.app, msg))
	_, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}

func (s workspaceConfigService) renderWorkspaceMenuCard(sessionKey string) map[string]any {
	var sess *state.Session
	if s.app.store != nil {
		sess = appState(s.app).session(sessionKey)
	}
	currentID := defaultWorkspaceID(s.app)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		currentID = sess.WorkspaceID
	}
	currentWS := config.FindWorkspace(s.app.cfg, currentID)
	bodyLines := []string{"当前工作区: `" + currentID + "`"}
	bodyLines = newBackendConfigurationService(s.app).appendBackendWorkspaceSummaryLines(bodyLines, currentWS)
	buttons := make([]feishu.Button, 0, 6)
	selectOptions := make([]selectStaticOption, 0, len(s.app.cfg.Workspaces))
	for _, ws := range s.app.cfg.Workspaces {
		label := ws.ID
		if ws.ID == currentID {
			label = "当前 · " + ws.ID
		}
		selectOptions = append(selectOptions, selectStaticOption{
			Text:  label,
			Value: ws.ID,
		})
	}
	buttons = append(buttons,
		feishu.Button{
			Text: submenuCommandLabel("新建工作区", "/workspace new"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.new",
				"session_key": sessionKey,
			},
		},
		feishu.Button{
			Text: submenuCommandLabel("从仓库创建", "/workspace clone"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.clone",
				"session_key": sessionKey,
			},
		},
	)
	buttons = append(buttons, newBackendConfigurationService(s.app).backendWorkspaceConfigButtons(sessionKey)...)
	buttons = append(buttons,
		feishu.Button{
			Text: submenuCommandLabel("删除工作区", "/workspace delete"),
			Type: "default",
			Value: map[string]any{
				"action":      "workspace.delete.menu",
				"session_key": sessionKey,
			},
		},
		feishu.Button{
			Text: "返回上一级",
			Type: "default",
			Value: map[string]any{
				"action":      "menu.root",
				"session_key": sessionKey,
			},
		},
	)
	card := newMarkdownBodyCard("工作区管理", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{"tag": "markdown", "content": menuCardBody("menu.workspace", strings.Join(bodyLines, "\n"))})
	appendMarkdownBodyCardElement(card, buildSelectStaticElement(
		"workspace_select",
		"list",
		map[string]any{"action": "workspace.use.select", "session_key": sessionKey},
		selectOptions,
		currentID,
	))
	for _, row := range buildMarkdownBodyCardActionElements(buttons) {
		appendMarkdownBodyCardElement(card, row)
	}
	return card
}

func workspaceSandboxOptions() []workspaceSettingOption {
	return []workspaceSettingOption{
		{Value: "read-only", Label: "read-only"},
		{Value: "workspace-write", Label: "workspace-write"},
		{Value: "danger-full-access", Label: "danger-full-access"},
	}
}

func workspaceApprovalPolicyOptions() []workspaceSettingOption {
	return []workspaceSettingOption{
		{Value: "untrusted", Label: "untrusted"},
		{Value: "on-request", Label: "on-request"},
		{Value: "never", Label: "never"},
	}
}

func (s workspaceConfigService) currentWorkspaceForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace) {
	sessionKey = makeSessionKey(s.app, msg)
	sess = appState(s.app).session(sessionKey)
	workspaceID := defaultWorkspaceID(s.app)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	return sessionKey, sess, config.FindWorkspace(s.app.cfg, workspaceID)
}

func (s workspaceConfigService) currentThreadForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace, threadID string, err error) {
	sessionKey, sess, ws = newWorkspaceConfigService(s.app).currentWorkspaceForMessage(msg)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return sessionKey, sess, ws, "", fmt.Errorf("%s", primaryConversationMissingLabel(configuredBackend(s.app)))
	}
	return sessionKey, sess, ws, strings.TrimSpace(sess.ActiveThreadID), nil
}

func (s workspaceConfigService) showWorkspaceSandboxMenu(msg *feishu.InboundMessage) error {
	card, err := newWorkspaceConfigService(s.app).renderWorkspaceSandboxMenuCard(makeSessionKey(s.app, msg))
	if err != nil {
		return err
	}
	_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}

func (s workspaceConfigService) renderWorkspaceSandboxMenuCard(sessionKey string) (map[string]any, error) {
	var sess *state.Session
	if s.app.store != nil {
		sess = appState(s.app).session(sessionKey)
	}
	workspaceID := defaultWorkspaceID(s.app)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(s.app.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	body := "配置当前工作区默认 sandbox。\n\n当前工作区: `" + ws.ID + "`\n当前值: `" + ws.SandboxMode + "`"
	buttons := make([]feishu.Button, 0, len(workspaceSandboxOptions())+1)
	for _, opt := range workspaceSandboxOptions() {
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
	return s.app.feishu.SimpleStatusCard("配置 Sandbox", "blue", menuCardBody("workspace.sandbox.menu", body), buttons), nil
}

func (s workspaceConfigService) showWorkspacePolicyMenu(msg *feishu.InboundMessage) error {
	card, err := newWorkspaceConfigService(s.app).renderWorkspacePolicyMenuCard(makeSessionKey(s.app, msg))
	if err != nil {
		return err
	}
	_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return err
}

func (s workspaceConfigService) renderWorkspacePolicyMenuCard(sessionKey string) (map[string]any, error) {
	var sess *state.Session
	if s.app.store != nil {
		sess = appState(s.app).session(sessionKey)
	}
	workspaceID := defaultWorkspaceID(s.app)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(s.app.cfg, workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	body := "配置当前工作区默认 approval policy。\n\n当前工作区: `" + ws.ID + "`\n当前值: `" + ws.ApprovalPolicy + "`"
	buttons := make([]feishu.Button, 0, len(workspaceApprovalPolicyOptions())+1)
	for _, opt := range workspaceApprovalPolicyOptions() {
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
	return s.app.feishu.SimpleStatusCard("配置 Policy", "blue", menuCardBody("workspace.policy.menu", body), buttons), nil
}
