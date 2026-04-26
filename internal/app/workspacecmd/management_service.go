package workspacecmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/app/appcore"
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
		SelectedCWD: appcore.FirstNonEmpty(func() string {
			if ws == nil {
				return ""
			}
			return strings.TrimSpace(ws.Cwd)
		}(), "/"),
	}
	return s.BeginWorkspaceNewWithPayload(msg, sessionKey, payload)
}

// BeginWorkspaceNewWithPayload starts the new workspace creation flow with a pre-filled payload.
func (s *ManagementService) BeginWorkspaceNewWithPayload(msg *feishu.InboundMessage, sessionKey string, payload NewPayload) error {
	requestID, err := s.NextLocalID("workspace")
	if err != nil {
		return err
	}
	card := s.RenderNewCard(sessionKey, requestID, payload)
	msgID, err := s.App.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.App, msg.ChatType))
	if err != nil {
		return err
	}
	return s.SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_new",
		SessionKey:  sessionKey,
		OwnerUserID: msg.UserID,
		FeishuMsgID: msgID,
		PayloadJSON: appcore.MustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
}

// CreateWorkspaceNewPending creates a pending new workspace request.
func (s *ManagementService) CreateWorkspaceNewPending(sessionKey, userID, feishuMsgID string, payload NewPayload) (string, error) {
	requestID, err := s.NextLocalID("workspace")
	if err != nil {
		return "", err
	}
	if err := s.SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_new",
		SessionKey:  sessionKey,
		OwnerUserID: userID,
		FeishuMsgID: strings.TrimSpace(feishuMsgID),
		PayloadJSON: appcore.MustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	}); err != nil {
		return "", err
	}
	return requestID, nil
}

// DefaultWorkspaceCloneRoot returns the default root for clone operations.
func (s *ManagementService) DefaultWorkspaceCloneRoot(ws *config.Workspace) string {
	return "/"
}

// DefaultWorkspaceCloneParent returns the default parent directory for clone operations.
func (s *ManagementService) DefaultWorkspaceCloneParent(ws *config.Workspace) string {
	if ws != nil && strings.TrimSpace(ws.Cwd) != "" {
		return filepath.Dir(strings.TrimSpace(ws.Cwd))
	}
	if strings.TrimSpace(s.App.ConfigPath()) != "" {
		return filepath.Dir(strings.TrimSpace(s.App.ConfigPath()))
	}
	return "."
}

// WorkspaceByCWD finds a workspace by its CWD path.
func (s *ManagementService) WorkspaceByCWD(targetDir string) *config.Workspace {
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" {
		return nil
	}
	cleanTarget := filepath.Clean(targetDir)
	for i := range s.App.Config().Workspaces {
		ws := &s.App.Config().Workspaces[i]
		if filepath.Clean(strings.TrimSpace(ws.Cwd)) == cleanTarget {
			return ws
		}
	}
	return nil
}

// WorkspaceByIDAndCWD finds a workspace by both ID and CWD.
func (s *ManagementService) WorkspaceByIDAndCWD(workspaceID, targetDir string) *config.Workspace {
	ws := config.FindWorkspace(s.App.Config(), strings.TrimSpace(workspaceID))
	if ws == nil || !sameWorkspaceCWD(targetDir, ws.Cwd) {
		return nil
	}
	return ws
}

// CreateWorkspaceAndSwitch creates a new workspace and switches to it.
func (s *ManagementService) CreateWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, id, name, cwd string) error {
	s.App.ConfigMu().Lock()
	if config.FindWorkspace(s.App.Config(), id) != nil {
		s.App.ConfigMu().Unlock()
		return fmt.Errorf("workspace %q 已存在", id)
	}
	s.App.Config().Workspaces = append(s.App.Config().Workspaces, config.Workspace{
		ID:             id,
		Name:           name,
		Cwd:            cwd,
		Model:          "",
		ApprovalPolicy: "on-request",
		SandboxMode:    "workspace-write",
	})
	if err := s.App.Config().Normalize(filepath.Dir(s.App.ConfigPath())); err != nil {
		s.App.Config().Workspaces = s.App.Config().Workspaces[:len(s.App.Config().Workspaces)-1]
		s.App.ConfigMu().Unlock()
		return err
	}
	if err := config.Save(s.App.ConfigPath(), s.App.Config()); err != nil {
		s.App.Config().Workspaces = s.App.Config().Workspaces[:len(s.App.Config().Workspaces)-1]
		s.App.ConfigMu().Unlock()
		return err
	}
	ws := config.FindWorkspace(s.App.Config(), id)
	s.App.ConfigMu().Unlock()
	sess := s.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, ChatID: chatID, ChatType: chatType, OwnerUserID: userID}
	}
	s.SwitchSessionWorkspace(sess, id)
	if err := s.SaveSession(sess); err != nil {
		return err
	}
	if !s.SessionHasInFlight(sess) && ws != nil {
		s.runAsyncThreadBinding(sessionKey, id, ws)
	}
	return nil
}

// UpdateWorkspaceDefaults updates a workspace configuration field and saves.
func (s *ManagementService) UpdateWorkspaceDefaults(workspaceID string, mutate func(*config.Workspace)) (*config.Workspace, error) {
	s.App.ConfigMu().Lock()
	defer s.App.ConfigMu().Unlock()
	ws := config.FindWorkspace(s.App.Config(), workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	mutate(ws)
	if err := s.App.Config().Normalize(filepath.Dir(s.App.ConfigPath())); err != nil {
		return nil, err
	}
	if err := config.Save(s.App.ConfigPath(), s.App.Config()); err != nil {
		return nil, err
	}
	return config.FindWorkspace(s.App.Config(), workspaceID), nil
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
		parentDir = s.DefaultWorkspaceCloneParent(ws)
	}
	workspaceID, targetDir, err := s.CloneWorkspaceInParent(
		context.Background(),
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
	return s.App.Feishu().ReplyText(context.Background(), msg.MessageID, reply, appcore.ReplyInThreadEnabled(s.App, msg.ChatType))
}

// PrepareWorkspaceClone validates and prepares a clone operation.
func (s *ManagementService) PrepareWorkspaceClone(repoURL, explicitID, parentDir string) (*ClonePlan, error) {
	repoName, err := CloneRepoName(repoURL)
	if err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(explicitID)
	if workspaceID == "" {
		workspaceID = CloneDefaultID(repoName)
		if workspaceID == "" {
			return nil, fmt.Errorf("无法从 git 地址推导 workspace_id，请手动指定")
		}
	}
	parentDir = strings.TrimSpace(parentDir)
	if parentDir == "" {
		return nil, fmt.Errorf("请先选择父目录")
	}
	targetName := repoName
	if strings.TrimSpace(explicitID) != "" {
		targetName = workspaceID
	}
	targetDir := filepath.Join(parentDir, targetName)
	if _, statErr := os.Stat(targetDir); statErr == nil {
		if existingWS := s.WorkspaceByCWD(targetDir); existingWS != nil {
			return nil, &CloneExistingWorkspaceError{
				WorkspaceID: existingWS.ID,
				TargetDir:   targetDir,
			}
		}
		return nil, &CloneExistingDirError{
			WorkspaceID: workspaceID,
			TargetDir:   targetDir,
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if config.FindWorkspace(s.App.Config(), workspaceID) != nil {
		return nil, fmt.Errorf("workspace %q 已存在，请指定新的 workspace_id", workspaceID)
	}
	return &ClonePlan{
		RepoName:    repoName,
		WorkspaceID: workspaceID,
		TargetDir:   targetDir,
	}, nil
}

// SetWorkspaceCloneOperation sets a clone operation for tracking.
func (s *ManagementService) SetWorkspaceCloneOperation(requestID string, op *CloneOperation) {
	if s.SetCloneOp != nil {
		s.SetCloneOp(requestID, op)
	}
}

// GetWorkspaceCloneOperation gets a clone operation by request ID.
func (s *ManagementService) GetWorkspaceCloneOperation(requestID string) *CloneOperation {
	if s.GetCloneOp != nil {
		return s.GetCloneOp(requestID)
	}
	return nil
}

// ClearWorkspaceCloneOperation clears a clone operation.
func (s *ManagementService) ClearWorkspaceCloneOperation(requestID string) {
	if s.ClearCloneOp != nil {
		s.ClearCloneOp(requestID)
	}
}

// FinishWorkspaceCloneSubmit completes a clone operation (called in a goroutine).
func (s *ManagementService) FinishWorkspaceCloneSubmit(ctx context.Context, op *CloneOperation, requestID, messageID, sessionKey, userID, chatID, chatType, parentDir string, payload ClonePayload) {
	defer s.ClearWorkspaceCloneOperation(requestID)
	slog.Debug("workspace clone started",
		"request_id", requestID,
		"message_id", messageID,
		"session_key", sessionKey,
		"repo_url", payload.RepoURL,
		"parent_dir", parentDir,
	)
	workspaceID, targetDir, err := s.CloneWorkspaceInParent(
		ctx,
		sessionKey,
		userID,
		chatID,
		chatType,
		payload.RepoURL,
		payload.DraftID,
		parentDir,
		func(line string) {
			s.noteWorkspaceCloneProgress(op, requestID, messageID, payload, parentDir, line)
		},
	)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Warn("workspace clone canceled",
				"request_id", requestID,
				"message_id", messageID,
				"session_key", sessionKey,
				"repo_url", payload.RepoURL,
				"parent_dir", parentDir,
			)
			payload.SelectedParentDir = parentDir
			payload.ErrorMessage = ""
			_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
				req.Status = "resolved"
				req.PayloadJSON = appcore.MustJSON(payload)
				req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
			})
			if strings.TrimSpace(messageID) != "" {
				s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderCloneCanceledCard(sessionKey, payload, parentDir, op.Snapshot()))
			}
			return
		}
		var takeoverErr *CloneTakeoverError
		if errors.As(err, &takeoverErr) {
			slog.Warn("workspace clone needs manual takeover",
				"request_id", requestID,
				"message_id", messageID,
				"session_key", sessionKey,
				"repo_url", payload.RepoURL,
				"parent_dir", parentDir,
				"target_dir", takeoverErr.TargetDir,
				"workspace_id", takeoverErr.WorkspaceID,
				"error", takeoverErr.Err,
			)
			payload.SelectedParentDir = parentDir
			payload.DraftID = appcore.FirstNonEmpty(strings.TrimSpace(payload.DraftID), strings.TrimSpace(takeoverErr.WorkspaceID))
			if takeoverErr.Err != nil {
				payload.ErrorMessage = takeoverErr.Err.Error()
			} else {
				payload.ErrorMessage = err.Error()
			}
			_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
				req.Status = "resolved"
				req.PayloadJSON = appcore.MustJSON(payload)
				req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
			})
			if strings.TrimSpace(messageID) != "" {
				_ = s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderCloneManualHintCard(sessionKey, payload.DraftID, takeoverErr.TargetDir, payload.ErrorMessage))
			}
			return
		}
		slog.Warn("workspace clone failed",
			"request_id", requestID,
			"message_id", messageID,
			"session_key", sessionKey,
			"repo_url", payload.RepoURL,
			"parent_dir", parentDir,
			"error", err,
		)
		payload.SelectedParentDir = parentDir
		payload.ErrorMessage = err.Error()
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = "pending"
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		})
		if strings.TrimSpace(messageID) != "" {
			_ = s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderCloneCard(sessionKey, requestID, payload))
		}
		return
	}
	slog.Debug("workspace clone completed",
		"request_id", requestID,
		"message_id", messageID,
		"session_key", sessionKey,
		"workspace_id", workspaceID,
		"target_dir", targetDir,
	)
	payload.SelectedParentDir = parentDir
	payload.ErrorMessage = ""
	_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = "resolved"
		req.PayloadJSON = appcore.MustJSON(payload)
	})
	if strings.TrimSpace(messageID) != "" {
		_ = s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderCloneSuccessCard(sessionKey, workspaceID, targetDir))
	}
}

// CompleteWorkspaceSandboxSet handles sandbox mode setting.
func (s *ManagementService) CompleteWorkspaceSandboxSet(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range SandboxOptions() {
		if opt.Value == sandboxMode {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 sandbox"}}, nil
	}
	_, err := s.updateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.SandboxMode = sandboxMode
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	// Re-render the sandbox menu card
	card, renderErr := s.renderSandboxMenuCard(sessionKey)
	if renderErr != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: renderErr.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 sandbox"},
		Card:  rawCard(card),
	}, nil
}

// CompleteWorkspacePolicySet handles approval policy setting.
func (s *ManagementService) CompleteWorkspacePolicySet(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	valid := false
	for _, opt := range ApprovalPolicyOptions() {
		if opt.Value == approvalPolicy {
			valid = true
			break
		}
	}
	if !valid {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "不支持的 policy"}}, nil
	}
	_, err := s.updateWorkspaceDefaults(workspaceID, func(w *config.Workspace) {
		w.ApprovalPolicy = approvalPolicy
	})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	card, renderErr := s.renderPolicyMenuCard(sessionKey)
	if renderErr != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: renderErr.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已更新 policy"},
		Card:  rawCard(card),
	}, nil
}

// CompleteWorkspaceUse handles workspace use action.
// Thread binding runs asynchronously so the Feishu card callback returns
// immediately instead of blocking on backend RPCs.
func (s *ManagementService) CompleteWorkspaceUse(action *feishu.CardAction, sessionKey, workspaceID string) (*callback.CardActionTriggerResponse, error) {
	ws := config.FindWorkspace(s.App.Config(), workspaceID)
	if ws == nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "工作区不存在"}}, nil
	}
	sess := s.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, OwnerUserID: action.UserID, ChatID: action.ChatID}
	}
	s.SwitchSessionWorkspace(sess, workspaceID)
	_ = s.SaveSession(sess)
	if !s.SessionHasInFlight(sess) {
		s.runAsyncThreadBinding(sessionKey, workspaceID, ws)
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已切换工作区"},
		Card:  rawCard(s.RenderMenuCard(sessionKey)),
	}, nil
}

// runAsyncThreadBinding runs EnsureWorkspaceThreadBinding asynchronously.
func (s *ManagementService) runAsyncThreadBinding(sessionKey, workspaceID string, ws *config.Workspace) {
	runner := s.RunAsync
	if runner == nil {
		runner = func(fn func()) { go fn() }
	}
	runner(func() {
		sess := s.GetSession(sessionKey)
		if sess == nil {
			return
		}
		binding, err := s.EnsureWorkspaceThreadBinding(sessionKey, sess, ws)
		if err != nil {
			slog.Warn("workspace action thread binding failed",
				"session_key", sessionKey,
				"workspace_id", workspaceID,
				"cwd", ws.Cwd,
				"error", err,
			)
		} else if binding != nil {
			slog.Debug("workspace thread binding completed",
				"session_key", sessionKey,
				"workspace_id", workspaceID,
				"thread_id", binding.ThreadID,
			)
		}
		if s.OnAsyncDone != nil {
			s.OnAsyncDone()
		}
	})
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
	requestID, err := s.NextLocalID("workspace")
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	msg := s.CommandMessageFromAction(action, sessionKey, "/workspace clone")
	_, _, ws := s.currentWorkspaceForMessage(msg)
	payload := ClonePayload{
		RootPath:          s.DefaultWorkspaceCloneRoot(ws),
		SelectedParentDir: appcore.FirstNonEmpty(strings.TrimSpace(s.DefaultWorkspaceCloneParent(ws)), "/"),
	}
	if err := s.SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_clone",
		SessionKey:  sessionKey,
		OwnerUserID: action.UserID,
		FeishuMsgID: strings.TrimSpace(action.MessageID),
		PayloadJSON: appcore.MustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "请填写 git 地址"},
		Card:  rawCard(s.RenderCloneCard(sessionKey, requestID, payload)),
	}, nil
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
		currentPath = appcore.FirstNonEmpty(strings.TrimSpace(s.DefaultWorkspaceCloneParent(ws)), "/")
	}
	payload.Picker = &PathPickerPayload{
		Mode:        PathPickerModeDirectory,
		Style:       PathPickerStyleDropdown,
		RootPath:    appcore.FirstNonEmpty(strings.TrimSpace(payload.RootPath), "/"),
		CurrentPath: currentPath,
	}
	_ = s.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = appcore.MustJSON(payload) })
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开父目录选择"},
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
		snapshot := op.RequestCancel()
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = "cancelling"
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		})
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
	currentPath := appcore.FirstNonEmpty(strings.TrimSpace(payload.SelectedCWD), "/")
	payload.Picker = &PathPickerPayload{
		Mode:        PathPickerModeDirectory,
		Style:       PathPickerStyleDropdown,
		RootPath:    appcore.FirstNonEmpty(strings.TrimSpace(payload.RootPath), "/"),
		CurrentPath: currentPath,
	}
	_ = s.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = appcore.MustJSON(payload) })
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开目录选择"},
		Card:  rawCard(s.RenderNewCard(pending.SessionKey, requestID, payload)),
	}, nil
}

// CompleteWorkspaceNewSubmit handles new workspace submit action.
func (s *ManagementService) CompleteWorkspaceNewSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	requestID, _ := action.ActionValue["request_id"].(string)
	pending := s.Pending(requestID)
	if pending == nil || pending.Kind != "workspace_new" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "工作区创建请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个工作区请求"}}, nil
	}
	payload := MergeNewFormValues(NewPayloadFromPending(pending), action.FormValue)
	id := strings.TrimSpace(payload.DraftID)
	if id == "" {
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = appcore.MustJSON(payload) })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请填写 workspace_id"},
			Card:  rawCard(s.RenderNewCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	cwd := strings.TrimSpace(payload.SelectedCWD)
	if cwd == "" {
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = appcore.MustJSON(payload) })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请先选择目录"},
			Card:  rawCard(s.RenderNewCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	name := strings.TrimSpace(payload.DraftName)
	if name == "" {
		name = id
	}
	if existingWS := s.WorkspaceByIDAndCWD(id, cwd); existingWS != nil {
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = "resolved"
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
		})
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "工作区已存在且目录一致，可直接切换"},
			Card:  rawCard(s.RenderSwitchExistingCard(pending.SessionKey, existingWS.ID, existingWS.Cwd, NewExistingWorkspaceNotice())),
		}, nil
	}
	sess := s.GetSession(pending.SessionKey)
	chatID := action.ChatID
	chatType := ""
	if sess != nil {
		chatID = appcore.FirstNonEmpty(chatID, sess.ChatID)
		chatType = sess.ChatType
	}
	if err := s.CreateWorkspaceAndSwitch(pending.SessionKey, action.UserID, chatID, chatType, id, name, cwd); err != nil {
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = appcore.MustJSON(payload) })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(s.RenderNewCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = "resolved"
		req.PayloadJSON = appcore.MustJSON(payload)
	})
	body := "已创建并切换到工作区 `" + id + "`\n\ncwd: `" + cwd + "`"
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已创建工作区"},
		Card:  rawCard(s.App.Feishu().SimpleStatusCard("工作区已创建", "green", body, nil)),
	}, nil
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
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = appcore.MustJSON(payload) })
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请填写 git 地址"},
			Card:  rawCard(s.RenderCloneCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	msg := s.CommandMessageFromAction(action, pending.SessionKey, "/workspace clone")
	sessionKey, _, ws := s.currentWorkspaceForMessage(msg)
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if parentDir == "" {
		parentDir = appcore.FirstNonEmpty(strings.TrimSpace(s.DefaultWorkspaceCloneParent(ws)), "/")
	}
	payload.SelectedParentDir = parentDir
	messageID := appcore.FirstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID))
	if status := strings.TrimSpace(pending.Status); status == "processing" || status == "cancelling" {
		snapshot := CloneProgressSnapshot{State: status}
		if op := s.GetWorkspaceCloneOperation(requestID); op != nil {
			snapshot = op.Snapshot()
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "正在从仓库创建工作区"},
			Card:  rawCard(s.RenderClonePreparingCard(requestID, payload, parentDir, snapshot)),
		}, nil
	}
	if _, err := s.PrepareWorkspaceClone(payload.RepoURL, payload.DraftID, parentDir); err != nil {
		var existingWorkspaceErr *CloneExistingWorkspaceError
		if errors.As(err, &existingWorkspaceErr) {
			_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
				req.Status = "resolved"
				req.PayloadJSON = appcore.MustJSON(payload)
				req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
			})
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "info", Content: "目标目录已经由现有工作区接管，可直接切换"},
				Card:  rawCard(s.RenderCloneSwitchExistingCard(pending.SessionKey, existingWorkspaceErr.WorkspaceID, existingWorkspaceErr.TargetDir)),
			}, nil
		}
		var existingDirErr *CloneExistingDirError
		if errors.As(err, &existingDirErr) {
			_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
				req.Status = "resolved"
				req.PayloadJSON = appcore.MustJSON(payload)
				req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
			})
			takeoverPayload := NewTakeoverPayloadWithNotice(existingDirErr.WorkspaceID, existingDirErr.TargetDir, NewTakeoverNotice(existingDirErr.TargetDir))
			newRequestID, createErr := s.CreateWorkspaceNewPending(pending.SessionKey, action.UserID, "", takeoverPayload)
			if createErr != nil {
				return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: createErr.Error()}}, nil
			}
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "info", Content: "clone 目标目录已存在，已打开预填好的新建工作区"},
				Card:  rawCard(s.RenderNewCard(pending.SessionKey, newRequestID, takeoverPayload)),
			}, nil
		}
		payload.ErrorMessage = err.Error()
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = "pending"
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		})
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(s.RenderCloneCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	op := NewCloneOperation(cancel)
	s.SetWorkspaceCloneOperation(requestID, op)
	_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = "processing"
		req.PayloadJSON = appcore.MustJSON(payload)
		req.FeishuMsgID = appcore.FirstNonEmpty(strings.TrimSpace(req.FeishuMsgID), messageID)
		req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
	})
	go s.FinishWorkspaceCloneSubmit(
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
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已开始从仓库创建工作区"},
		Card:  rawCard(s.RenderClonePreparingCard(requestID, payload, parentDir, op.Snapshot())),
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

// CompleteClaudeWorkspacePermissionMenu handles Claude workspace permission menu action.
func (s *ManagementService) CompleteClaudeWorkspacePermissionMenu(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.CompleteMenuCommand(action, sessionKey, "/workspace permissions", "menu.workspace")
}

// CompleteWorkspaceNewText handles text input for new workspace creation.
func (s *ManagementService) CompleteWorkspaceNewText(msg *feishu.InboundMessage, pending *state.PendingRequest) error {
	payload := NewPayloadFromPending(pending)
	parts := strings.Fields(strings.TrimSpace(msg.Text))
	if len(parts) < 1 {
		return fmt.Errorf("格式错误，需发送: workspace_id [name]")
	}
	id := parts[0]
	cwd := strings.TrimSpace(payload.SelectedCWD)
	name := id
	if cwd == "" && len(parts) >= 2 {
		cwd = parts[1]
		if len(parts) > 2 {
			name = strings.Join(parts[2:], " ")
		}
	} else if len(parts) > 1 {
		name = strings.Join(parts[1:], " ")
	}
	if strings.TrimSpace(cwd) == "" {
		return fmt.Errorf("请先选择目录")
	}
	sessionKey := appcore.MakeSessionKey(s.App, msg)
	if existingWS := s.WorkspaceByIDAndCWD(id, cwd); existingWS != nil {
		payload.DraftID = id
		payload.DraftName = name
		_ = s.UpdatePending(pending.ID, func(req *state.PendingRequest) {
			req.Status = "resolved"
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
		})
		if pending.FeishuMsgID != "" {
			_ = s.App.Feishu().PatchCard(context.Background(), pending.FeishuMsgID, s.RenderSwitchExistingCard(sessionKey, existingWS.ID, existingWS.Cwd, NewExistingWorkspaceNotice()))
		}
		return s.App.Feishu().ReplyText(context.Background(), msg.MessageID, "工作区已存在且目录一致，可直接切换到 "+existingWS.ID, appcore.ReplyInThreadEnabled(s.App, msg.ChatType))
	}
	if err := s.CreateWorkspaceAndSwitch(sessionKey, msg.UserID, msg.ChatID, msg.ChatType, id, name, cwd); err != nil {
		return err
	}
	_ = s.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = "resolved" })
	if pending.FeishuMsgID != "" {
		_ = s.App.Feishu().PatchCard(context.Background(), pending.FeishuMsgID, s.App.Feishu().SimpleStatusCard("工作区已创建", "green", "已创建并切换到工作区 `"+id+"`\n\ncwd: `"+cwd+"`", nil))
	}
	return s.App.Feishu().ReplyText(context.Background(), msg.MessageID, "已创建并切换到工作区 "+id, appcore.ReplyInThreadEnabled(s.App, msg.ChatType))
}

// --- private helpers ---

func (s *ManagementService) currentWorkspaceForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace) {
	sessionKey = appcore.MakeSessionKey(s.App, msg)
	sess = s.GetSession(sessionKey)
	workspaceID := appcore.DefaultWorkspaceID(s.App)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	return sessionKey, sess, config.FindWorkspace(s.App.Config(), workspaceID)
}

func (s *ManagementService) CloneWorkspaceInParent(ctx context.Context, sessionKey, userID, chatID, chatType, repoURL, explicitID, parentDir string, report CloneProgressReporter) (string, string, error) {
	plan, err := s.PrepareWorkspaceClone(repoURL, explicitID, parentDir)
	if err != nil {
		return "", "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(filepath.Dir(plan.TargetDir), 0o755); err != nil {
		return "", "", err
	}
	if err := s.GitClone(ctx, strings.TrimSpace(repoURL), plan.TargetDir, report); err != nil {
		return "", "", err
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if err := s.CreateWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, plan.WorkspaceID, plan.WorkspaceID, plan.TargetDir); err != nil {
		return "", "", &CloneTakeoverError{
			WorkspaceID: plan.WorkspaceID,
			TargetDir:   plan.TargetDir,
			Err:         err,
		}
	}
	return plan.WorkspaceID, plan.TargetDir, nil
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
	if err := s.App.Feishu().PatchCard(context.Background(), messageID, card); err != nil {
		slog.Warn("workspace clone progress patch failed",
			"request_id", requestID,
			"message_id", messageID,
			"error", err,
		)
	}
}

func (s *ManagementService) updateWorkspaceDefaults(workspaceID string, mutate func(*config.Workspace)) (*config.Workspace, error) {
	s.App.ConfigMu().Lock()
	defer s.App.ConfigMu().Unlock()
	ws := config.FindWorkspace(s.App.Config(), workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", workspaceID)
	}
	mutate(ws)
	if err := s.App.Config().Normalize(filepath.Dir(s.App.ConfigPath())); err != nil {
		return nil, err
	}
	if err := config.Save(s.App.ConfigPath(), s.App.Config()); err != nil {
		return nil, err
	}
	return config.FindWorkspace(s.App.Config(), workspaceID), nil
}

// renderSandboxMenuCard is a helper that re-renders the sandbox menu card.
func (s *ManagementService) renderSandboxMenuCard(sessionKey string) (map[string]any, error) {
	var sess *state.Session
	if s.GetSession != nil {
		sess = s.GetSession(sessionKey)
	}
	workspaceID := appcore.DefaultWorkspaceID(s.App)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(s.App.Config(), workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	body := "配置当前工作区默认 sandbox。\n\n当前工作区: `" + ws.ID + "`\n当前值: `" + ws.SandboxMode + "`"
	buttons := make([]feishu.Button, 0, len(SandboxOptions())+1)
	for _, opt := range SandboxOptions() {
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
	if s.FormatMenuBody != nil {
		bodyText = s.FormatMenuBody("workspace.sandbox.menu", body)
	}
	return s.App.Feishu().SimpleStatusCard("配置 Sandbox", "blue", bodyText, buttons), nil
}

// renderPolicyMenuCard is a helper that re-renders the policy menu card.
func (s *ManagementService) renderPolicyMenuCard(sessionKey string) (map[string]any, error) {
	var sess *state.Session
	if s.GetSession != nil {
		sess = s.GetSession(sessionKey)
	}
	workspaceID := appcore.DefaultWorkspaceID(s.App)
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = sess.WorkspaceID
	}
	ws := config.FindWorkspace(s.App.Config(), workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	body := "配置当前工作区默认 approval policy。\n\n当前工作区: `" + ws.ID + "`\n当前值: `" + ws.ApprovalPolicy + "`"
	buttons := make([]feishu.Button, 0, len(ApprovalPolicyOptions())+1)
	for _, opt := range ApprovalPolicyOptions() {
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
	if s.FormatMenuBody != nil {
		bodyText = s.FormatMenuBody("workspace.policy.menu", body)
	}
	return s.App.Feishu().SimpleStatusCard("配置 Policy", "blue", bodyText, buttons), nil
}
