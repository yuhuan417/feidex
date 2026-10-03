package workspacecmd

import (
	"context"
	appworkspace "feidex/internal/application/workspace"
	"feidex/internal/domain/conversation"
	"log/slog"
	"strings"
	"time"

	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// BeginWorkspaceNew starts the new workspace creation flow.
func (s *ManagementService) BeginWorkspaceNew(msg *feishu.InboundMessage) error {
	sessionKey, _, ws := s.currentWorkspaceForMessage(msg)
	payload := NewPayload{
		RootPath: "/",
		SelectedCWD: firstNonEmpty(func() string {
			if ws == nil {
				return ""
			}
			return strings.TrimSpace(ws.Cwd)
		}(), "/"),
	}
	return s.BeginWorkspaceNewWithPayload(msg, sessionKey, payload)
}

// BeginWorkspaceWorktree starts the git worktree workspace creation flow.
func (s *ManagementService) BeginWorkspaceWorktree(msg *feishu.InboundMessage, branchName, workspaceID string) error {
	sessionKey, _, ws := s.currentWorkspaceForMessage(msg)
	payload := s.Deps.Planning.DefaultWorkspaceWorktreePayload(ws, branchName, workspaceID)
	return s.BeginWorkspaceWorktreeWithPayload(msg, sessionKey, payload)
}

// BeginWorkspaceWorktreeWithPayload starts the worktree flow with a pre-filled payload.
func (s *ManagementService) BeginWorkspaceWorktreeWithPayload(msg *feishu.InboundMessage, sessionKey string, payload WorktreePayload) error {
	pending, err := s.Deps.Workflow.Open("workspace_worktree", sessionKey, msg.UserID, "", payload)
	if err != nil {
		return err
	}
	msgID, err := s.Deps.OutboundCapability().ReplyCard(s.Deps.Context(), msg.MessageID, s.RenderWorktreeCard(sessionKey, pending.ID, payload), false)
	if err != nil {
		return err
	}
	return s.Deps.Forms.SaveDraft(pending.ID, nil, "", 0, msgID)
}

// BeginWorkspaceNewWithPayload starts the new workspace creation flow with a pre-filled payload.
func (s *ManagementService) BeginWorkspaceNewWithPayload(msg *feishu.InboundMessage, sessionKey string, payload NewPayload) error {
	pending, err := s.Deps.Workflow.Open("workspace_new", sessionKey, msg.UserID, "", payload)
	if err != nil {
		return err
	}
	msgID, err := s.Deps.OutboundCapability().ReplyCard(s.Deps.Context(), msg.MessageID, s.RenderNewCard(sessionKey, pending.ID, payload), false)
	if err != nil {
		return err
	}
	return s.Deps.Forms.SaveDraft(pending.ID, nil, "", 0, msgID)
}

// CreateWorkspaceNewPending creates a pending new workspace request.
func (s *ManagementService) CreateWorkspaceNewPending(sessionKey, userID, feishuMsgID string, payload NewPayload) (string, error) {
	pending, err := s.Deps.Workflow.Open("workspace_new", sessionKey, userID, feishuMsgID, payload)
	if err != nil {
		return "", err
	}
	return pending.ID, nil
}

// CreateWorkspaceAndSwitch creates a new workspace and switches to it.
func (s *ManagementService) creationRequest(sessionKey, userID, chatID, chatType string) appworkspace.SwitchRequest {
	sess := s.GetSession(sessionKey)
	if sess == nil {
		sess = &conversation.Session{Key: sessionKey, ChatID: chatID, ChatType: chatType, OwnerUserID: userID}
	}
	return appworkspace.SwitchRequest{Session: sess}
}

func (s *ManagementService) CreateWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, id, name, cwd string) error {
	_, err := s.Deps.Workflow.Create(s.creationRequest(sessionKey, userID, chatID, chatType), id, name, cwd)
	return err
}

// CloneWorkspaceAndSwitch clones a repository and switches to the new workspace.
func (s *ManagementService) CloneWorkspaceAndSwitch(msg *feishu.InboundMessage, repoURL, explicitID string) error {
	return s.CloneWorkspaceAndSwitchInSelectedParent(msg, repoURL, explicitID, "")
}

// CloneWorkspaceAndSwitchInSelectedParent clones a repository in a specific parent directory.
func (s *ManagementService) CloneWorkspaceAndSwitchInSelectedParent(msg *feishu.InboundMessage, repoURL, explicitID, parentDir string) error {
	if msg == nil {
		return nil
	}
	sessionKey, _, ws := s.currentWorkspaceForMessage(msg)
	if strings.TrimSpace(parentDir) == "" {
		parentDir = s.Deps.Planning.DefaultWorkspaceCloneParent(ws)
	}
	workspaceID, targetDir, err := s.CloneWorkspaceInParent(
		s.Deps.Context(),
		sessionKey,
		msg.UserID,
		msg.ChatID,
		msg.ChatType,
		repoURL,
		explicitID,
		parentDir,
		nil,
	)
	if err != nil {
		return err
	}
	reply := "已从仓库创建并切换到工作区 " + workspaceID + "\n" + "cwd: " + targetDir
	return s.Deps.OutboundCapability().ReplyText(s.Deps.Context(), msg.MessageID, reply, false)
}

// SetWorkspaceCloneOperation sets a clone operation for tracking.
func (s *ManagementService) SetWorkspaceCloneOperation(requestID string, op *CloneOperation) {
	s.SetCloneOp(requestID, op)
}

// GetWorkspaceCloneOperation gets a clone operation by request ID.
func (s *ManagementService) GetWorkspaceCloneOperation(requestID string) *CloneOperation {
	return s.GetCloneOp(requestID)
}

// ClearWorkspaceCloneOperation clears a clone operation.
func (s *ManagementService) ClearWorkspaceCloneOperation(requestID string) {
	s.ClearCloneOp(requestID)
}

// FinishWorkspaceCloneSubmit completes a clone operation (called in a goroutine).
func (s *ManagementService) FinishWorkspaceCloneSubmit(ctx context.Context, op *CloneOperation, requestID, messageID, sessionKey, userID, chatID, chatType, parentDir string, payload ClonePayload) {

	defer s.ClearWorkspaceCloneOperation(requestID)
	plan, err := s.Deps.Planning.PrepareWorkspaceClonePayload(payload, parentDir)
	if err != nil {
		slog.Warn("workspace clone plan failed", "error", err)
		return
	}
	out, err := s.Deps.Workflow.FinishClone(ctx, requestID, s.creationRequest(sessionKey, userID, chatID, chatType), payload, plan, func(line string) {
		s.noteWorkspaceCloneProgress(op, requestID, messageID, payload, parentDir, line)
	})
	if err != nil {
		slog.Warn("workspace clone result save failed", "error", err)
		return
	}
	var card map[string]any
	switch out.Outcome {
	case appworkspace.CreationCancelled:
		card = s.RenderCloneCanceledCard(sessionKey, out.Payload, parentDir, op.Snapshot())
	case appworkspace.CreationTakeover:
		card = s.RenderCloneManualHintCard(sessionKey, out.WorkspaceID, out.TargetDir, out.Error)
	case appworkspace.CreationFailed:
		card = s.RenderCloneCard(sessionKey, requestID, out.Payload)
	default:

		card = s.RenderCloneSuccessCard(sessionKey, out.WorkspaceID, out.TargetDir)
	}
	if strings.TrimSpace(messageID) != "" {
		_ = s.Deps.OutboundCapability().PatchCard(s.Deps.Context(), messageID, card)
	}
}

// FinishWorkspaceWorktreeSubmit completes a worktree operation in the background.
func (s *ManagementService) FinishWorkspaceWorktreeSubmit(ctx context.Context, op *CloneOperation, requestID, messageID, sessionKey, userID, chatID, chatType string, payload WorktreePayload, plan *WorktreePlan) {

	defer s.ClearWorkspaceCloneOperation(requestID)
	out, err := s.Deps.Workflow.FinishWorktree(ctx, requestID, s.creationRequest(sessionKey, userID, chatID, chatType), payload, plan)
	if err != nil {
		slog.Warn("workspace worktree result save failed", "error", err)
		return
	}
	var card map[string]any
	switch out.Outcome {
	case appworkspace.CreationCancelled:
		card = s.RenderWorktreeCanceledCard(sessionKey, out.Payload, plan, op.Snapshot())
	case appworkspace.CreationTakeover:
		card = s.RenderWorktreeManualHintCard(sessionKey, out.WorkspaceID, out.TargetDir, out.Error)
	case appworkspace.CreationFailed:
		card = s.RenderWorktreeCard(sessionKey, requestID, out.Payload)
	default:

		card = s.RenderWorktreeSuccessCard(sessionKey, out.WorkspaceID, out.TargetDir)
	}
	if strings.TrimSpace(messageID) != "" {
		_ = s.Deps.OutboundCapability().PatchCard(s.Deps.Context(), messageID, card)
	}
}

// CompleteWorkspaceSandboxSet handles sandbox mode setting.
func (s *ManagementService) CompleteWorkspaceSandboxSet(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	return s.Deps.PermissionDriver().CompleteWorkspaceSandboxSet(sessionKey, workspaceID, sandboxMode, appbackend.WorkspacePermissionUpdateDeps{
		Settings:          s.Deps.Settings,
		RenderSandboxMenu: s.renderSandboxMenuCard,
		RenderPolicyMenu:  s.renderPolicyMenuCard,
	})
}

// CompleteWorkspacePolicySet handles approval policy setting.
func (s *ManagementService) CompleteWorkspacePolicySet(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	return s.Deps.PermissionDriver().CompleteWorkspacePolicySet(sessionKey, workspaceID, approvalPolicy, appbackend.WorkspacePermissionUpdateDeps{
		Settings:          s.Deps.Settings,
		RenderSandboxMenu: s.renderSandboxMenuCard,
		RenderPolicyMenu:  s.renderPolicyMenuCard,
	})
}

// CompleteWorkspaceMultiAgentSet handles multi-agent mode setting.
func (s *ManagementService) CompleteWorkspaceMultiAgentSet(action *feishu.CardAction, sessionKey, workspaceID, mode string) (*callback.CardActionTriggerResponse, error) {
	return s.Deps.PermissionDriver().CompleteWorkspaceMultiAgentSet(sessionKey, workspaceID, mode, appbackend.WorkspacePermissionUpdateDeps{
		Settings:             s.Deps.Settings,
		RenderSandboxMenu:    s.renderSandboxMenuCard,
		RenderPolicyMenu:     s.renderPolicyMenuCard,
		RenderMultiAgentMenu: s.renderMultiAgentMenuCard,
	})
}

func (s *ManagementService) CompleteWorkspacePermissionModeSet(action *feishu.CardAction, sessionKey, workspaceID, rawMode string) (*callback.CardActionTriggerResponse, error) {
	return s.Deps.PermissionDriver().CompleteWorkspacePermissionModeSet(sessionKey, workspaceID, rawMode, appbackend.WorkspacePermissionModeUpdateDeps{
		Permissions: s.Deps,
		Session:     s.GetSession,
		Settings:    s.Deps.Settings,
		RenderPermissionMenu: func(sessionKey string) (map[string]any, error) {
			return s.Deps.SettingsRenderer.RenderWorkspacePermissionModeMenuCard(sessionKey)
		},
	})
}

// CompleteWorkspaceUse handles workspace use action.
// Thread binding runs asynchronously so the Feishu card callback returns
// immediately instead of blocking on backend RPCs.
func (s *ManagementService) CompleteWorkspaceUse(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	ws := config.FindWorkspace(s.Deps.Config(), workspaceID)
	if ws == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "工作区不存在"}}, nil
	}
	sess := s.GetSession(sessionKey)
	if sess == nil {
		sess = &conversation.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	if _, err := s.Deps.Workflow.Switch(appworkspace.SwitchRequest{Session: sess, WorkspaceID: workspaceID}, true); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已切换工作区"},
		Card:  rawCard(s.RenderMenuCard(sessionKey)),
	}, nil
}

// CompleteWorkspaceUseExisting handles workspace use existing action.
func (s *ManagementService) CompleteWorkspaceUseExisting(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	return s.CompleteWorkspaceUse(action, sessionKey, workspaceID)
}

// CompleteWorkspaceNew handles workspace new action.
func (s *ManagementService) CompleteWorkspaceNew(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.CompleteMenuCommand(action, sessionKey, "/workspace new", "menu.workspace")
}

// CompleteWorkspaceClone handles workspace clone action.
func (s *ManagementService) CompleteWorkspaceClone(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := s.CommandMessageFromAction(action, sessionKey, "/workspace clone")
	_, _, ws := s.currentWorkspaceForMessage(msg)
	payload := ClonePayload{RootPath: s.Deps.Planning.DefaultWorkspaceCloneRoot(ws), SelectedParentDir: firstNonEmpty(s.Deps.Planning.DefaultWorkspaceCloneParent(ws), "/"), CloneMode: CloneModeWorkspace}
	pending, err := s.Deps.Workflow.Open("workspace_clone", sessionKey, action.UserID, action.MessageID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "请填写 git 地址"}, Card: rawCard(s.RenderCloneCard(sessionKey, pending.ID, payload))}, nil
}

// CompleteWorkspaceWorktree opens the worktree creation card from the menu.
func (s *ManagementService) CompleteWorkspaceWorktree(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	msg := s.CommandMessageFromAction(action, sessionKey, "/workspace new worktree")
	_, _, ws := s.currentWorkspaceForMessage(msg)
	payload := s.Deps.Planning.DefaultWorkspaceWorktreePayload(ws, "", "")
	pending, err := s.Deps.Workflow.Open("workspace_worktree", sessionKey, action.UserID, action.MessageID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已打开 Worktree 创建表单"}, Card: rawCard(s.RenderWorktreeCard(sessionKey, pending.ID, payload))}, nil
}

// CompleteWorkspaceNewTakeover handles workspace new takeover action.
func (s *ManagementService) CompleteWorkspaceNewTakeover(action *feishu.CardAction, sessionKey, workspaceID, targetDir string) (*callback.CardActionTriggerResponse, error) {
	payload := NewTakeoverPayload(workspaceID, targetDir)
	if strings.TrimSpace(payload.SelectedCWD) == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "缺少可接管的目录"}}, nil
	}
	requestID, err := s.CreateWorkspaceNewPending(sessionKey, action.UserID, "", payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "clone 目标目录已存在，已转为预填好的新建工作区"},
		Card:  rawCard(s.RenderNewCard(sessionKey, requestID, payload)),
	}, nil
}

// CompleteWorkspaceCloneUseExisting handles clone use existing action.
func (s *ManagementService) CompleteWorkspaceCloneUseExisting(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	return s.CompleteWorkspaceUse(action, sessionKey, workspaceID)
}

// CompleteWorkspaceClonePickDir handles clone pick directory action.
func (s *ManagementService) CompleteWorkspaceClonePickDir(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil || pending.Kind != "workspace_clone" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := MergeCloneFormValues(ClonePayloadFromPending(pending), action.FormValue)
	currentPath := strings.TrimSpace(payload.SelectedParentDir)
	if currentPath == "" {
		msg := s.CommandMessageFromAction(action, pending.SessionKey, "/workspace clone")
		_, _, ws := s.currentWorkspaceForMessage(msg)
		currentPath = firstNonEmpty(strings.TrimSpace(s.Deps.Planning.DefaultWorkspaceCloneParent(ws)), "/")
	}
	payload = s.Deps.Planning.DefaultCloneWorktreePayload(payload, currentPath)
	payload.Picker = &PathPickerPayload{
		Mode:        PathPickerModeDirectory,
		Style:       PathPickerStyleDropdown,
		RootPath:    firstNonEmpty(strings.TrimSpace(payload.RootPath), "/"),
		CurrentPath: currentPath,
	}
	if err := s.Deps.Forms.SaveDraft(requestID, payload, "", 0, ""); err != nil {
		return nil, err
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开父目录选择"},
		Card:  rawCard(s.RenderCloneCard(pending.SessionKey, requestID, payload)),
	}, nil
}

// CompleteWorkspaceCloneRefresh refreshes clone form visibility after changing
// the clone mode without starting the clone operation.
func (s *ManagementService) CompleteWorkspaceCloneRefresh(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil || pending.Kind != "workspace_clone" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := MergeCloneFormValues(ClonePayloadFromPending(pending), action.FormValue)
	payload.ErrorMessage = ""
	payload.Picker = nil
	msg := s.CommandMessageFromAction(action, pending.SessionKey, "/workspace clone")
	_, _, ws := s.currentWorkspaceForMessage(msg)
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if parentDir == "" {
		parentDir = firstNonEmpty(strings.TrimSpace(s.Deps.Planning.DefaultWorkspaceCloneParent(ws)), "/")
	}
	payload.SelectedParentDir = parentDir
	payload = s.Deps.Planning.DefaultCloneWorktreePayload(payload, parentDir)
	if err := s.Deps.Forms.SaveDraft(requestID, payload, state.PendingRequestStatusPending.String(), 10*time.Minute, ""); err != nil {
		return nil, err
	}
	toast := "已更新创建方式"
	if CloneCreatesWorktree(payload) {
		toast = "已显示 worktree 字段"
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: toast},
		Card:  rawCard(s.RenderCloneCard(pending.SessionKey, requestID, payload)),
	}, nil
}

// CompleteWorkspaceCloneCancel handles clone cancel action.
func (s *ManagementService) CompleteWorkspaceCloneCancel(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil || pending.Kind != "workspace_clone" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区克隆请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := ClonePayloadFromPending(pending)
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if op := s.GetWorkspaceCloneOperation(requestID); op != nil {
		if err := s.Deps.Forms.SaveDraft(requestID, payload, state.PendingRequestStatusCancelling.String(), 10*time.Minute, ""); err != nil {
			return nil, err
		}
		snapshot := op.RequestCancel()
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "已请求取消仓库克隆"},
			Card:  rawCard(s.RenderClonePreparingCard(requestID, payload, parentDir, snapshot)),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: "当前没有进行中的仓库克隆"},
		Card:  rawCard(s.RenderCloneCard(pending.SessionKey, requestID, payload)),
	}, nil
}

// CompleteWorkspaceNewPickDir handles new workspace pick directory action.
func (s *ManagementService) CompleteWorkspaceNewPickDir(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil || pending.Kind != "workspace_new" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := MergeNewFormValues(NewPayloadFromPending(pending), action.FormValue)
	currentPath := firstNonEmpty(strings.TrimSpace(payload.SelectedCWD), "/")
	payload.Picker = &PathPickerPayload{
		Mode:        PathPickerModeDirectory,
		Style:       PathPickerStyleDropdown,
		RootPath:    firstNonEmpty(strings.TrimSpace(payload.RootPath), "/"),
		CurrentPath: currentPath,
	}
	if err := s.Deps.Forms.SaveDraft(requestID, payload, "", 0, ""); err != nil {
		return nil, err
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开目录选择"},
		Card:  rawCard(s.RenderNewCard(pending.SessionKey, requestID, payload)),
	}, nil
}

// CompleteWorkspaceNewSubmit handles new workspace submit action.
func (s *ManagementService) CompleteWorkspaceNewSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	payload := MergeNewFormValues(NewPayloadFromPending(pending), action.FormValue)
	msg := s.CommandMessageFromAction(action, pending.SessionKey, "/workspace new")
	out, err := s.Deps.Workflow.SubmitNew(requestID, action.UserID, payload, s.creationRequest(pending.SessionKey, msg.UserID, msg.ChatID, msg.ChatType))
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}, Card: rawCard(s.RenderNewCard(pending.SessionKey, requestID, out.Payload))}, nil
	}
	if out.Existing {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "工作区已存在且目录一致，可直接切换"}, Card: rawCard(s.RenderSwitchExistingCard(pending.SessionKey, out.WorkspaceID, out.TargetDir, NewExistingWorkspaceNotice()))}, nil
	}
	body := "已创建并切换到工作区 `" + out.WorkspaceID + "`\n\ncwd: `" + out.TargetDir + "`"
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已创建工作区"}, Card: rawCard(s.Deps.Renderer().SimpleStatusCard("工作区已创建", "green", body, nil))}, nil
}

// CompleteWorkspaceCloneSubmit handles clone submit action.
func (s *ManagementService) CompleteWorkspaceCloneSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil || pending.Kind != "workspace_clone" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区克隆请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := MergeCloneFormValues(ClonePayloadFromPending(pending), action.FormValue)
	payload.ErrorMessage = ""
	if strings.TrimSpace(payload.RepoURL) == "" {
		if err := s.Deps.Forms.SaveDraft(requestID, payload, "", 0, ""); err != nil {
			return nil, err
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请填写 git 地址"},
			Card:  rawCard(s.RenderCloneCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	msg := s.CommandMessageFromAction(action, pending.SessionKey, "/workspace clone")
	defaultMessageChatType(msg)
	sessionKey, _, ws := s.currentWorkspaceForMessage(msg)
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if parentDir == "" {
		parentDir = firstNonEmpty(strings.TrimSpace(s.Deps.Planning.DefaultWorkspaceCloneParent(ws)), "/")
	}
	payload.SelectedParentDir = parentDir
	messageID := firstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID))
	if status := state.NormalizePendingRequestStatus(pending.Status); status == state.PendingRequestStatusProcessing || status == state.PendingRequestStatusCancelling {
		snapshot := CloneProgressSnapshot{State: status.String()}
		if op := s.GetWorkspaceCloneOperation(requestID); op != nil {
			snapshot = op.Snapshot()
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "正在从仓库创建工作区"},
			Card:  rawCard(s.RenderClonePreparingCard(requestID, payload, parentDir, snapshot)),
		}, nil
	}
	payload, plan, preparation, err := s.Deps.Workflow.PrepareClone(requestID, action.UserID, payload, ws, appworkspace.SwitchRequest{})
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return s.renderWorkspacePreparation(pending, payload, preparation, false)
	}
	ctx, cancel := context.WithCancel(s.Deps.Context())
	op := NewCloneOperation(cancel)
	if err := s.Deps.Workflow.Start(requestID, pending.Kind, action.UserID, messageID, payload); err != nil {
		cancel()
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	s.SetWorkspaceCloneOperation(requestID, op)
	s.runWorkspaceAsync(func() {
		s.FinishWorkspaceCloneSubmit(
			ctx,
			op,
			requestID,
			messageID,
			sessionKey,
			msg.UserID,
			msg.ChatID,
			msg.ChatType,
			parentDir,
			payload,
		)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已开始从仓库创建工作区"},
		Card:  rawCard(s.RenderClonePreparingCard(requestID, payload, parentDir, op.Snapshot())),
	}, nil
}

// CompleteWorkspaceWorktreeSubmit handles worktree submit action.
func (s *ManagementService) CompleteWorkspaceWorktreeSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil || pending.Kind != "workspace_worktree" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "Worktree 创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := MergeWorktreeFormValues(WorktreePayloadFromPending(pending), action.FormValue)
	payload.ErrorMessage = ""
	if status := state.NormalizePendingRequestStatus(pending.Status); status == state.PendingRequestStatusProcessing || status == state.PendingRequestStatusCancelling {
		snapshot := CloneProgressSnapshot{State: status.String()}
		if op := s.GetWorkspaceCloneOperation(requestID); op != nil {
			snapshot = op.Snapshot()
		}
		plan, _ := s.Deps.Planning.PrepareWorkspaceWorktree(payload)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "正在创建 Worktree 工作区"},
			Card:  rawCard(s.RenderWorktreePreparingCard(requestID, payload, plan, snapshot)),
		}, nil
	}
	payload, plan, preparation, err := s.Deps.Workflow.PrepareWorktree(requestID, action.UserID, payload, appworkspace.SwitchRequest{})
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return s.renderWorkspacePreparation(pending, payload, preparation, true)
	}
	messageID := firstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID))
	ctx, cancel := context.WithCancel(s.Deps.Context())
	op := NewCloneOperation(cancel)
	if err := s.Deps.Workflow.Start(requestID, pending.Kind, action.UserID, messageID, payload); err != nil {
		cancel()
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	s.SetWorkspaceCloneOperation(requestID, op)
	msg := s.CommandMessageFromAction(action, pending.SessionKey, "/workspace new worktree")
	defaultMessageChatType(msg)
	s.runWorkspaceAsync(func() {
		s.FinishWorkspaceWorktreeSubmit(ctx, op, requestID, messageID, pending.SessionKey, msg.UserID, msg.ChatID, msg.ChatType, payload, plan)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已开始创建 Worktree 工作区"},
		Card:  rawCard(s.RenderWorktreePreparingCard(requestID, payload, plan, op.Snapshot())),
	}, nil
}

// CompleteWorkspaceWorktreeCancel handles worktree cancel action.
func (s *ManagementService) CompleteWorkspaceWorktreeCancel(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil || pending.Kind != "workspace_worktree" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "Worktree 创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := WorktreePayloadFromPending(pending)
	plan, _ := s.Deps.Planning.PrepareWorkspaceWorktree(payload)
	if op := s.GetWorkspaceCloneOperation(requestID); op != nil {
		snapshot := op.RequestCancel()
		if err := s.Deps.Forms.SaveDraft(requestID, payload, state.PendingRequestStatusCancelling.String(), 10*time.Minute, ""); err != nil {
			return nil, err
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "已请求取消 Worktree 创建"},
			Card:  rawCard(s.RenderWorktreePreparingCard(requestID, payload, plan, snapshot)),
		}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "warning", Content: "当前没有进行中的 Worktree 创建"},
		Card:  rawCard(s.RenderWorktreeCard(pending.SessionKey, requestID, payload)),
	}, nil
}

// CompleteWorkspaceSandboxMenu handles sandbox menu action.
func (s *ManagementService) CompleteWorkspaceSandboxMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.CompleteMenuCommand(action, sessionKey, "/workspace sandbox", "menu.workspace")
}

// CompleteWorkspacePolicyMenu handles policy menu action.
func (s *ManagementService) CompleteWorkspacePolicyMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.CompleteMenuCommand(action, sessionKey, "/workspace policy", "menu.workspace")
}

// CompleteWorkspaceMultiAgentMenu handles multi-agent menu action.
func (s *ManagementService) CompleteWorkspaceMultiAgentMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.CompleteMenuCommand(action, sessionKey, "/workspace multiagent", "menu.workspace")
}

// CompleteClaudeWorkspacePermissionMenu handles Claude workspace permission menu action.
func (s *ManagementService) CompleteClaudeWorkspacePermissionMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.CompleteMenuCommand(action, sessionKey, "/workspace permissions", "menu.workspace")
}

// CompleteWorkspaceNewText handles text input for new workspace creation.
func (s *ManagementService) CompleteWorkspaceNewText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	payload, err := appworkspace.NewPayloadFromText(NewPayloadFromPending(pending), msg.Text)
	if err != nil {
		return err
	}
	key := makeSessionKey(s.Deps, msg)
	out, err := s.Deps.Workflow.SubmitNew(pending.ID, msg.UserID, payload, s.creationRequest(key, msg.UserID, msg.ChatID, msg.ChatType))
	if err != nil {
		return err
	}
	var card map[string]any
	reply := "已创建并切换到工作区 " + out.WorkspaceID
	if out.Existing {
		card = s.RenderSwitchExistingCard(key, out.WorkspaceID, out.TargetDir, NewExistingWorkspaceNotice())
		reply = "工作区已存在且目录一致，可直接切换到 " + out.WorkspaceID
	} else {
		card = s.Deps.Renderer().SimpleStatusCard("工作区已创建", "green", "已创建并切换到工作区 `"+out.WorkspaceID+"`\n\ncwd: `"+out.TargetDir+"`", nil)
	}
	if pending.FeishuMsgID != "" {
		_ = s.Deps.OutboundCapability().PatchCard(s.Deps.Context(), pending.FeishuMsgID, card)
	}
	return s.Deps.OutboundCapability().ReplyText(s.Deps.Context(), msg.MessageID, reply, false)
}

// --- private helpers ---

func (s *ManagementService) currentWorkspaceForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *conversation.Session, ws *config.Workspace) {
	sessionKey = makeSessionKey(s.Deps, msg)
	sess = s.GetSession(sessionKey)
	workspaceID := selectedWorkspaceIDForMessage(s.Deps, msg, sess)
	return sessionKey, sess, config.FindWorkspace(s.Deps.Config(), workspaceID)
}

func defaultMessageChatType(msg *feishu.InboundMessage) {
	if msg == nil || strings.TrimSpace(msg.ChatType) != "" || strings.TrimSpace(msg.ChatID) == "" {
		return
	}
	msg.ChatType = "p2p"
}

func (s *ManagementService) runWorkspaceAsync(fn func()) {
	if fn == nil {
		return
	}
	s.deps.Async.RunAsync(fn)
}

func (s *ManagementService) CloneWorkspaceInParent(ctx context.Context, sessionKey, userID, chatID, chatType, repoURL, explicitID, parentDir string, report CloneProgressReporter) (string, string, error) {
	payload := ClonePayload{RepoURL: strings.TrimSpace(repoURL), DraftID: strings.TrimSpace(explicitID), CloneMode: CloneModeWorkspace}
	return s.CloneWorkspacePayloadInParent(ctx, sessionKey, userID, chatID, chatType, payload, parentDir, report)
}

func (s *ManagementService) CloneWorkspacePayloadInParent(ctx context.Context, sessionKey, userID, chatID, chatType string, payload ClonePayload, parentDir string, report CloneProgressReporter) (string, string, error) {
	if ctx == nil {
		ctx = s.Deps.Context()
	}
	result, err := s.Deps.Workflow.Clone(ctx, s.creationRequest(sessionKey, userID, chatID, chatType), payload, parentDir, report)
	return result.WorkspaceID, result.TargetDir, err
}

func (s *ManagementService) noteWorkspaceCloneProgress(op *CloneOperation, requestID, messageID string, payload ClonePayload, parentDir, line string) {
	if op == nil {
		return
	}
	snapshot, shouldPatch := op.RecordProgress(line)
	if shouldPatch {
		s.patchWorkspaceCloneProgressCard(messageID, requestID, payload, parentDir, snapshot)
	}
}

func (s *ManagementService) patchWorkspaceCloneProgressCard(messageID, requestID string, payload ClonePayload, parentDir string, snapshot CloneProgressSnapshot) {
	if strings.TrimSpace(messageID) == "" {
		return
	}
	card := s.RenderClonePreparingCard(requestID, payload, parentDir, snapshot)
	if err := s.Deps.OutboundCapability().PatchCard(s.Deps.Context(), messageID, card); err != nil {
		slog.Warn("workspace clone progress patch failed",
			"request_id", requestID,
			"message_id", messageID,
			"error", err,
		)
	}
}

// renderSandboxMenuCard is a helper that re-renders the sandbox menu card.
func (s *ManagementService) renderSandboxMenuCard(sessionKey string) (map[string]any, error) {
	return s.Deps.SettingsRenderer.RenderWorkspaceSandboxMenuCard(sessionKey)
}

// renderPolicyMenuCard is a helper that re-renders the policy menu card.
func (s *ManagementService) renderPolicyMenuCard(sessionKey string) (map[string]any, error) {
	return s.Deps.SettingsRenderer.RenderWorkspacePolicyMenuCard(sessionKey)
}

// renderMultiAgentMenuCard is a helper that re-renders the multi-agent menu card.
func (s *ManagementService) renderMultiAgentMenuCard(sessionKey string) (map[string]any, error) {
	return s.Deps.SettingsRenderer.RenderWorkspaceMultiAgentMenuCard(sessionKey)
}

func (s *ManagementService) renderWorkspacePreparation(pending *state.PendingRequest, payload any, preparation appworkspace.Preparation, worktree bool) (*callback.CardActionTriggerResponse, error) {
	kind, toast := "warning", preparation.Error
	var card map[string]any
	switch preparation.Outcome {
	case appworkspace.CreationCompleted:
		kind, toast = "info", "目标目录已经由现有工作区接管，可直接切换"
		notice := NewExistingWorkspaceNotice()
		if worktree {
			notice = "worktree 目标目录已经由现有工作区接管。"
		}
		card = s.RenderSwitchExistingCard(pending.SessionKey, preparation.WorkspaceID, preparation.TargetDir, notice)
	case appworkspace.CreationTakeover:
		kind, toast = "info", "clone 目标目录已存在，已打开预填好的新建工作区"
		if worktree {
			toast = "worktree 目标目录已存在，已打开预填好的新建工作区"
		}
		card = s.RenderNewCard(pending.SessionKey, preparation.Takeover.ID, preparation.NewPayload)
	default:
		if worktree {
			card = s.RenderWorktreeCard(pending.SessionKey, pending.ID, payload.(WorktreePayload))
		} else {
			card = s.RenderCloneCard(pending.SessionKey, pending.ID, payload.(ClonePayload))
		}
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: kind, Content: toast}, Card: rawCard(card)}, nil
}
