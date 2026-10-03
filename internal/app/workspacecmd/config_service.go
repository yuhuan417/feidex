package workspacecmd

import (
	"context"
	"errors"
	configadapter "feidex/internal/adapter/config"
	appworkspace "feidex/internal/application/workspace"
	"feidex/internal/domain/conversation"
	"fmt"
	"strings"

	appbackend "feidex/internal/app/backend"
	"feidex/internal/config"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// CommandWorkspace dispatches the /workspace command.
func (s *ConfigService) CommandWorkspace(msg *feishu.InboundMessage, args []string, mgmt *ManagementService) error {
	if len(args) == 0 {
		return s.ShowWorkspaceMenu(msg)
	}
	sessionKey := makeSessionKey(s.Deps, msg)
	if args[0] == "list" {
		return s.ShowWorkspaceMenu(msg)
	}
	if args[0] == "new" {
		if len(args) >= 2 && strings.EqualFold(strings.TrimSpace(args[1]), "worktree") {
			branchName, workspaceID, err := ParseWorktreeArgs(args)
			if err != nil {
				return err
			}
			return mgmt.BeginWorkspaceWorktree(msg, branchName, workspaceID)
		}
		return mgmt.BeginWorkspaceNew(msg)
	}
	if args[0] == "clone" {
		if len(args) == 1 {
			action := s.CommandActionFromMessage(msg, map[string]any{"session_key": sessionKey})
			resp, err := mgmt.CompleteWorkspaceClone(action, sessionKey)
			if err != nil {
				return err
			}
			return s.ReplyCommandActionResponse(msg, resp)
		}
		repoURL, workspaceID, parentDir, err := ParseCloneArgs(args)
		if err != nil {
			return err
		}
		if parentDir != "" {
			err = mgmt.CloneWorkspaceAndSwitchInSelectedParent(msg, repoURL, workspaceID, parentDir)
		} else {
			err = mgmt.CloneWorkspaceAndSwitch(msg, repoURL, workspaceID)
		}
		var existingDirErr *CloneExistingDirError
		if errors.As(err, &existingDirErr) {
			return mgmt.BeginWorkspaceNewWithPayload(msg, sessionKey, NewTakeoverPayloadWithNotice(existingDirErr.WorkspaceID, existingDirErr.TargetDir, NewTakeoverNotice(existingDirErr.TargetDir)))
		}
		var existingWorkspaceErr *CloneExistingWorkspaceError
		if errors.As(err, &existingWorkspaceErr) {
			return s.ReplyCommandActionResponse(msg, &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "info", Content: "目标目录已经由现有工作区接管，可直接切换"},
				Card:  rawCard(s.RenderCloneSwitchExistingCard(sessionKey, existingWorkspaceErr.WorkspaceID, existingWorkspaceErr.TargetDir)),
			})
		}
		return err
	}
	if args[0] == "delete" {
		if len(args) == 1 {
			return s.ShowWorkspaceDeleteMenu(msg)
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: /workspace delete [ID]")
		}
		workspaceID := strings.TrimSpace(args[1])
		if err := s.DeleteWorkspace(sessionKey, workspaceID); err != nil {
			return err
		}
		reply := "已删除工作区 " + workspaceID + "，仅移除配置，未删除目录"
		return s.Deps.OutboundCapability().ReplyText(context.Background(), msg.MessageID, reply, false)
	}
	if args[0] == "permissions" || args[0] == "sandbox" || args[0] == "policy" || args[0] == "multiagent" {
		return s.Deps.PermissionDriver().HandleWorkspaceCommand(appbackend.WorkspacePermissionCommandRequest{
			Message:    msg,
			Args:       args,
			SessionKey: sessionKey,
			CurrentWorkspace: func(msg *feishu.InboundMessage) (string, *conversation.Session, *config.Workspace) {
				return s.CurrentWorkspaceForMessage(msg)
			},
			ShowWorkspaceSandboxMenu: func(msg *feishu.InboundMessage) error {
				return s.ShowWorkspaceSandboxMenu(msg)
			},
			ShowWorkspacePolicyMenu: func(msg *feishu.InboundMessage) error {
				return s.ShowWorkspacePolicyMenu(msg)
			},
			ShowWorkspacePermissionModeMenu: func(msg *feishu.InboundMessage) error {
				card, err := s.Deps.SettingsRenderer.RenderWorkspacePermissionModeMenuCard(sessionKey)
				if err != nil {
					return err
				}
				_, err = s.Deps.OutboundCapability().ReplyCard(context.Background(), msg.MessageID, card, false)
				return err
			},
			ShowWorkspaceMultiAgentMenu: func(msg *feishu.InboundMessage) error {
				return s.ShowWorkspaceMultiAgentMenu(msg)
			},
			CompleteWorkspaceSandboxSet: func(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
				return mgmt.CompleteWorkspaceSandboxSet(action, sessionKey, workspaceID, sandboxMode)
			},
			CompleteWorkspacePolicySet: func(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
				return mgmt.CompleteWorkspacePolicySet(action, sessionKey, workspaceID, approvalPolicy)
			},
			CompleteWorkspacePermissionModeSet: func(action *feishu.CardAction, sessionKey, workspaceID, rawMode string) (*callback.CardActionTriggerResponse, error) {
				return mgmt.CompleteWorkspacePermissionModeSet(action, sessionKey, workspaceID, rawMode)
			},
			CompleteWorkspaceMultiAgentSet: func(action *feishu.CardAction, sessionKey, workspaceID, mode string) (*callback.CardActionTriggerResponse, error) {
				return mgmt.CompleteWorkspaceMultiAgentSet(action, sessionKey, workspaceID, mode)
			},
			ReplyCommandActionResponse: s.ReplyCommandActionResponse,
			CommandActionFromMessage:   s.CommandActionFromMessage,
		})
	}
	if args[0] == "choose" {
		return s.ShowWorkspaceChooseMenu(msg)
	}
	if len(args) >= 2 && args[0] == "use" {
		ws := config.FindWorkspace(s.Deps.Config(), args[1])
		if ws == nil {
			return fmt.Errorf("workspace %q not found", args[1])
		}
		sess := s.GetSession(sessionKey)
		if sess == nil {
			sess = &conversation.Session{Key: sessionKey, ChatID: msg.ChatID, ChatType: msg.ChatType, OwnerUserID: msg.UserID}
		}
		if reason := workspaceSwitchBlockedReason(sess, s.SessionHasInFlight(sess)); reason != "" {
			return fmt.Errorf("%s", reason)
		}
		if err := setSelectedWorkspaceForMessage(s.Deps, msg, ws.ID); err != nil {
			return err
		}
		reply := "已切换工作区到 " + ws.ID
		if err := applyWorkspaceSwitch(s, sessionKey, sess, ws.ID); err != nil {
			return err
		}
		binding, err := s.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
		if err != nil {
			// Log warning but don't fail
			reply += s.BackendWorkspaceSwitchBindingFailureNotice()
			return s.Deps.OutboundCapability().ReplyText(context.Background(), msg.MessageID, reply, false)
		}
		reply += s.BackendWorkspaceSwitchBindingNotice(binding)
		return s.Deps.OutboundCapability().ReplyText(context.Background(), msg.MessageID, reply, false)
	}
	return fmt.Errorf("usage: %s", s.BackendWorkspaceCommandUsage())
}

// ShowWorkspaceMenu shows the workspace management menu.
func (s *ConfigService) ShowWorkspaceMenu(msg *feishu.InboundMessage) error {
	card := s.RenderMenuCard(makeSessionKey(s.Deps, msg))
	_, err := s.Deps.OutboundCapability().ReplyCard(context.Background(), msg.MessageID, card, false)
	return err
}

// ShowWorkspaceChooseMenu shows the workspace choose card with buttons.
func (s *ConfigService) ShowWorkspaceChooseMenu(msg *feishu.InboundMessage) error {
	card := s.RenderChooseMenuCard(makeSessionKey(s.Deps, msg))
	if card == nil {
		return s.ShowWorkspaceMenu(msg)
	}
	_, err := s.Deps.OutboundCapability().ReplyCard(context.Background(), msg.MessageID, card, false)
	return err
}

// CurrentWorkspaceForMessage returns the session key, session, and workspace for a message.
func (s *ConfigService) CurrentWorkspaceForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace) {
	sessionKey = makeSessionKey(s.Deps, msg)
	sess = s.GetSession(sessionKey)
	workspaceID := selectedWorkspaceIDForMessage(s.Deps, msg, sess)
	return sessionKey, sess, config.FindWorkspace(s.Deps.Config(), workspaceID)
}

// ShowWorkspaceSandboxMenu shows the sandbox configuration menu.
func (s *ConfigService) ShowWorkspaceSandboxMenu(msg *feishu.InboundMessage) error {
	card, err := s.RenderSandboxMenuCard(makeSessionKey(s.Deps, msg))
	if err != nil {
		return err
	}
	_, err = s.Deps.OutboundCapability().ReplyCard(context.Background(), msg.MessageID, card, false)
	return err
}

// ShowWorkspacePolicyMenu shows the policy configuration menu.
func (s *ConfigService) ShowWorkspacePolicyMenu(msg *feishu.InboundMessage) error {
	card, err := s.RenderPolicyMenuCard(makeSessionKey(s.Deps, msg))
	if err != nil {
		return err
	}
	_, err = s.Deps.OutboundCapability().ReplyCard(context.Background(), msg.MessageID, card, false)
	return err
}

// ShowWorkspaceMultiAgentMenu shows the multi-agent mode configuration menu.
func (s *ConfigService) ShowWorkspaceMultiAgentMenu(msg *feishu.InboundMessage) error {
	card, err := s.RenderMultiAgentMenuCard(makeSessionKey(s.Deps, msg))
	if err != nil {
		return err
	}
	_, err = s.Deps.OutboundCapability().ReplyCard(context.Background(), msg.MessageID, card, false)
	return err
}

// ShowWorkspaceDeleteMenu shows the workspace delete menu.
func (s *ConfigService) ShowWorkspaceDeleteMenu(msg *feishu.InboundMessage) error {
	card, err := s.RenderDeleteMenuCard(makeSessionKey(s.Deps, msg))
	if err != nil {
		return err
	}
	_, err = s.Deps.OutboundCapability().ReplyCard(context.Background(), msg.MessageID, card, false)
	return err
}

// ValidateWorkspaceDeletion validates that a workspace can be deleted.
func (s *ConfigService) ValidateWorkspaceDeletion(sessionKey, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return fmt.Errorf("请指定 workspace_id")
	}
	repository := configadapter.NewWorkspaceRepository(s.Deps)
	workspace, err := repository.Get(workspaceID)
	if err != nil {
		return err
	}
	if workspace == nil {
		return fmt.Errorf("workspace %q 不存在", workspaceID)
	}
	if len(repository.List()) <= 1 {
		return fmt.Errorf("至少保留一个 workspace")
	}
	currentID := selectedWorkspaceIDForSession(s.Deps, s.GetSession(sessionKey))
	if workspaceID == currentID {
		return fmt.Errorf("不能删除当前 workspace，请先切换到其他 workspace")
	}
	for _, sess := range s.Sessions() {
		if sess == nil || !s.SessionHasInFlight(sess) {
			continue
		}
		if SessionReferencesWorkspace(sess, workspaceID) {
			return fmt.Errorf("workspace %q 仍有运行中的任务，无法删除", workspaceID)
		}
	}
	if s.Deps.ConfigProvider != nil && s.Deps.Store() != nil {
		frontendID := strings.TrimSpace(s.Deps.FrontendID())
		legacyFallback := allowLegacyFallback(s.Deps)
		for _, binding := range s.Deps.Store().AllAgentBindings() {
			if binding == nil || (strings.TrimSpace(binding.FrontendID) != frontendID && !(strings.TrimSpace(binding.FrontendID) == "" && legacyFallback)) {
				continue
			}
			if strings.TrimSpace(binding.WorkspaceID) == workspaceID {
				return fmt.Errorf("workspace %q 仍被某个群里的当前 Bot 工作区配置使用，请先在对应群聊中用 /workspace use 切换", workspaceID)
			}
		}
	}
	return nil
}

// DeleteWorkspace deletes a workspace configuration.
func (s *ConfigService) DeleteWorkspace(sessionKey, workspaceID string) error {
	if err := s.ValidateWorkspaceDeletion(sessionKey, workspaceID); err != nil {
		return err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	fallbackID, err := (appworkspace.ConfigurationService{Repository: configadapter.NewWorkspaceRepository(s.Deps)}).Delete(workspaceID)
	if err != nil {
		return err
	}

	for _, sess := range s.Sessions() {
		if sess == nil {
			continue
		}
		updated := false
		if strings.TrimSpace(sess.WorkspaceID) == workspaceID {
			s.SwitchSessionWorkspace(sess, fallbackID)
			updated = true
		} else if strings.TrimSpace(sess.ActiveThreadWorkspaceID) == workspaceID {
			s.ClearSessionThreadCtx(sess)
			updated = true
		}
		if !updated {
			continue
		}
		s.ClearSessionLiveThread(sess.Key)
		if err := s.SaveSession(sess); err != nil {
			return err
		}
	}
	return nil
}

// CompleteWorkspaceDeleteMenu handles the workspace delete menu action.
func (s *ConfigService) CompleteWorkspaceDeleteMenu(sessionKey string) (*callback.CardActionTriggerResponse, error) {
	card, err := s.RenderDeleteMenuCard(sessionKey)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请选择要删除的工作区"},
		Card:  rawCard(card),
	}, nil
}

// CompleteWorkspaceDeletePrompt handles the workspace delete prompt action.
func (s *ConfigService) CompleteWorkspaceDeletePrompt(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	workspaceID = firstNonEmpty(strings.TrimSpace(workspaceID), strings.TrimSpace(action.Option))
	if err := s.ValidateWorkspaceDeletion(sessionKey, workspaceID); err != nil {
		card, renderErr := s.RenderDeleteMenuCard(sessionKey)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(card),
		}, nil
	}
	card, err := s.RenderDeleteConfirmCard(sessionKey, workspaceID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: "确认后只删除配置，不删除目录"},
		Card:  rawCard(card),
	}, nil
}

// CompleteWorkspaceDeleteConfirm handles the workspace delete confirm action.
func (s *ConfigService) CompleteWorkspaceDeleteConfirm(sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	if err := s.DeleteWorkspace(sessionKey, workspaceID); err != nil {
		card, renderErr := s.RenderDeleteMenuCard(sessionKey)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(card),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已删除工作区 " + strings.TrimSpace(workspaceID)},
		Card:  rawCard(s.RenderMenuCard(sessionKey)),
	}, nil
}

// CompleteMenuWorkspace handles the menu.workspace action.
func (s *ConfigService) CompleteMenuWorkspace(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuWorkspaceImpl(s, action, sessionKey)
}

func completeMenuWorkspaceImpl(s *ConfigService, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	resp, err := s.CompleteMenuCommand(action, sessionKey, "/workspace", "menu.root")
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	if resp != nil {
		return resp, nil
	}
	card := s.RenderMenuCard(sessionKey)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已执行 /workspace"},
		Card:  rawCard(card),
	}, nil
}
