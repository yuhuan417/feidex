package workspacecmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"feidex/internal/app/appcore"
	appbackend "feidex/internal/app/backend"
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

// BeginWorkspaceWorktree starts the git worktree workspace creation flow.
func (s *ManagementService) BeginWorkspaceWorktree(msg *feishu.InboundMessage, branchName, workspaceID string) error {
	sessionKey, _, ws := s.currentWorkspaceForMessage(msg)
	payload := s.DefaultWorkspaceWorktreePayload(msg, ws, branchName, workspaceID)
	return s.BeginWorkspaceWorktreeWithPayload(msg, sessionKey, payload)
}

// BeginWorkspaceWorktreeWithPayload starts the worktree flow with a pre-filled payload.
func (s *ManagementService) BeginWorkspaceWorktreeWithPayload(msg *feishu.InboundMessage, sessionKey string, payload WorktreePayload) error {
	requestID, err := s.NextLocalID("workspace")
	if err != nil {
		return err
	}
	card := s.RenderWorktreeCard(sessionKey, requestID, payload)
	msgID, err := s.App.Feishu().ReplyCard(context.Background(), msg.MessageID, card, appcore.ReplyInThreadEnabled(s.App, msg.ChatType))
	if err != nil {
		return err
	}
	return s.SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_worktree",
		SessionKey:  sessionKey,
		OwnerUserID: msg.UserID,
		FeishuMsgID: msgID,
		PayloadJSON: appcore.MustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
}

// DefaultWorkspaceWorktreePayload returns a mostly pre-filled worktree payload.
func (s *ManagementService) DefaultWorkspaceWorktreePayload(msg *feishu.InboundMessage, ws *config.Workspace, branchName, workspaceID string) WorktreePayload {
	baseID := ""
	if ws != nil {
		baseID = strings.TrimSpace(ws.ID)
	}
	baseProject := s.worktreeBaseProjectLabel(ws)
	botName := s.worktreeBotLabel()
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		branchName = SuggestedWorktreeBranch(baseProject, botName, "")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = SuggestedWorktreeID(baseProject, botName)
	}
	directoryName := workspaceID
	if repoRoot, err := gitRepoRoot(func() string {
		if ws == nil {
			return ""
		}
		return ws.Cwd
	}()); err == nil {
		branchName, workspaceID, directoryName = s.uniqueWorktreeDefaults(repoRoot, branchName, workspaceID, directoryName)
	}
	payload := WorktreePayload{
		BaseWorkspaceID: baseID,
		BranchName:      branchName,
		WorkspaceID:     workspaceID,
		DirectoryName:   directoryName,
	}
	if plan, err := s.PrepareWorkspaceWorktree(payload); err == nil {
		payload.TargetDir = plan.TargetDir
	}
	return payload
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
		Status:      state.PendingRequestStatusPending.String(),
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
		Status:      state.PendingRequestStatusPending.String(),
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
	sess := s.GetSession(sessionKey)
	if sess == nil {
		sess = &state.Session{Key: sessionKey, ChatID: chatID, ChatType: chatType, OwnerUserID: userID}
	}
	if reason := workspaceSwitchBlockedReason(sess, s.SessionHasInFlight(sess)); reason != "" {
		return fmt.Errorf("%s", reason)
	}
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
	if err := appcore.SetWorkspaceSelection(s.App, chatType, chatID, userID, id); err != nil {
		return err
	}
	if err := applyWorkspaceSwitch(s, sessionKey, sess, id); err != nil {
		return err
	}
	if ws != nil {
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
	payload := ClonePayload{RepoURL: strings.TrimSpace(repoURL), DraftID: strings.TrimSpace(explicitID), CloneMode: CloneModeWorkspace}
	return s.PrepareWorkspaceClonePayload(payload, parentDir)
}

// PrepareWorkspaceClonePayload validates and prepares a clone operation,
// including optional clone-then-worktree output.
func (s *ManagementService) PrepareWorkspaceClonePayload(payload ClonePayload, parentDir string) (*ClonePlan, error) {
	repoURL := strings.TrimSpace(payload.RepoURL)
	explicitID := strings.TrimSpace(payload.DraftID)
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
	if !CloneCreatesWorktree(payload) && config.FindWorkspace(s.App.Config(), workspaceID) != nil {
		return nil, fmt.Errorf("workspace %q 已存在，请指定新的 workspace_id", workspaceID)
	}
	plan := &ClonePlan{
		RepoName:    repoName,
		WorkspaceID: workspaceID,
		TargetDir:   targetDir,
	}
	if !CloneCreatesWorktree(payload) {
		return plan, nil
	}
	worktree, err := s.prepareCloneWorktreePlan(payload, repoName, parentDir, targetDir)
	if err != nil {
		return nil, err
	}
	plan.Worktree = worktree
	return plan, nil
}

// PrepareWorkspaceWorktree validates and prepares a worktree operation.
func (s *ManagementService) PrepareWorkspaceWorktree(payload WorktreePayload) (*WorktreePlan, error) {
	baseWorkspaceID := strings.TrimSpace(payload.BaseWorkspaceID)
	if baseWorkspaceID == "" {
		return nil, fmt.Errorf("请先选择基准工作区")
	}
	baseWS := config.FindWorkspace(s.App.Config(), baseWorkspaceID)
	if baseWS == nil {
		return nil, fmt.Errorf("基准工作区 %q 不存在", baseWorkspaceID)
	}
	baseRepoRoot, err := gitRepoRoot(strings.TrimSpace(baseWS.Cwd))
	if err != nil {
		return nil, fmt.Errorf("基准工作区不是可用的 Git 目录: %w", err)
	}
	branchName := strings.TrimSpace(payload.BranchName)
	if branchName == "" {
		return nil, fmt.Errorf("请填写新分支名")
	}
	if err := validateGitBranchName(branchName); err != nil {
		return nil, fmt.Errorf("分支名无效: %w", err)
	}
	if exists, err := gitBranchExists(baseRepoRoot, branchName); err != nil {
		return nil, fmt.Errorf("检查分支失败: %w", err)
	} else if exists {
		return nil, fmt.Errorf("分支 %q 已存在，请换一个新分支名", branchName)
	}
	workspaceID := strings.TrimSpace(payload.WorkspaceID)
	if workspaceID == "" {
		workspaceID = SuggestedWorktreeID(s.worktreeBaseProjectLabel(baseWS), s.worktreeBotLabel())
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("无法推导 workspace_id，请手动填写")
	}
	directoryName := strings.TrimSpace(payload.DirectoryName)
	if directoryName == "" {
		directoryName = workspaceID
	}
	if err := validateWorktreeDirectoryName(directoryName); err != nil {
		return nil, err
	}
	targetDir := filepath.Join(filepath.Dir(baseRepoRoot), directoryName)
	if existingWS := s.WorkspaceByCWD(targetDir); existingWS != nil {
		return nil, &CloneExistingWorkspaceError{WorkspaceID: existingWS.ID, TargetDir: targetDir}
	}
	if _, statErr := os.Stat(targetDir); statErr == nil {
		return nil, &CloneExistingDirError{WorkspaceID: workspaceID, TargetDir: targetDir}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if existing := config.FindWorkspace(s.App.Config(), workspaceID); existing != nil {
		if sameWorkspaceCWD(existing.Cwd, targetDir) {
			return nil, &CloneExistingWorkspaceError{WorkspaceID: existing.ID, TargetDir: targetDir}
		}
		return nil, fmt.Errorf("workspace %q 已存在，请换一个 workspace_id", workspaceID)
	}
	return &WorktreePlan{
		BaseWorkspaceID: baseWorkspaceID,
		BaseRepoRoot:    baseRepoRoot,
		BranchName:      branchName,
		WorkspaceID:     workspaceID,
		DirectoryName:   directoryName,
		TargetDir:       targetDir,
	}, nil
}

func clonePayloadWithPlan(payload ClonePayload, plan *ClonePlan) ClonePayload {
	payload.CloneMode = NormalizeCloneMode(payload.CloneMode)
	if plan == nil {
		return payload
	}
	if strings.TrimSpace(payload.DraftID) == "" {
		payload.DraftID = strings.TrimSpace(plan.WorkspaceID)
	}
	if plan.Worktree != nil {
		payload.CloneMode = CloneModeWorktree
		payload.WorktreeBranchName = strings.TrimSpace(plan.Worktree.BranchName)
		payload.WorktreeWorkspaceID = strings.TrimSpace(plan.Worktree.WorkspaceID)
		payload.WorktreeDirectoryName = strings.TrimSpace(plan.Worktree.DirectoryName)
		payload.WorktreeTargetDir = strings.TrimSpace(plan.Worktree.TargetDir)
	}
	return payload
}

// ClonePayloadWithPlan returns payload with inferred clone/worktree fields from
// a prepared plan. Callers use it before persisting the in-progress form state.
func (s *ManagementService) ClonePayloadWithPlan(payload ClonePayload, plan *ClonePlan) ClonePayload {
	return clonePayloadWithPlan(payload, plan)
}

// DefaultCloneWorktreePayload fills optional clone-then-worktree fields when
// the repo URL and parent directory are already known. It intentionally avoids
// hard validation so directory picking can stay lightweight.
func (s *ManagementService) DefaultCloneWorktreePayload(payload ClonePayload, parentDir string) ClonePayload {
	payload.CloneMode = NormalizeCloneMode(payload.CloneMode)
	if !CloneCreatesWorktree(payload) {
		return payload
	}
	repoName, err := CloneRepoName(payload.RepoURL)
	if err != nil {
		return payload
	}
	parentDir = strings.TrimSpace(parentDir)
	if parentDir == "" {
		parentDir = strings.TrimSpace(payload.SelectedParentDir)
	}
	baseProject := appcore.FirstNonEmpty(strings.TrimSpace(repoName), "workspace")
	botName := s.worktreeBotLabel()
	workspaceID := strings.TrimSpace(payload.WorktreeWorkspaceID)
	branchName := strings.TrimSpace(payload.WorktreeBranchName)
	directoryName := strings.TrimSpace(payload.WorktreeDirectoryName)
	if workspaceID == "" {
		workspaceID = SuggestedWorktreeID(baseProject, botName)
	}
	if branchName == "" {
		branchName = SuggestedWorktreeBranch(baseProject, botName, "")
	}
	if directoryName == "" {
		directoryName = workspaceID
	}
	if strings.TrimSpace(payload.WorktreeWorkspaceID) == "" && strings.TrimSpace(payload.WorktreeBranchName) == "" && strings.TrimSpace(payload.WorktreeDirectoryName) == "" && parentDir != "" {
		branchName, workspaceID, directoryName = s.uniqueCloneWorktreeDefaults(parentDir, branchName, workspaceID, directoryName)
	}
	payload.WorktreeWorkspaceID = workspaceID
	payload.WorktreeBranchName = branchName
	payload.WorktreeDirectoryName = directoryName
	if parentDir != "" && directoryName != "" {
		payload.WorktreeTargetDir = filepath.Join(parentDir, directoryName)
	}
	return payload
}

func (s *ManagementService) prepareCloneWorktreePlan(payload ClonePayload, repoName, parentDir, cloneTargetDir string) (*CloneWorktreePlan, error) {
	baseProject := appcore.FirstNonEmpty(strings.TrimSpace(repoName), "workspace")
	botName := s.worktreeBotLabel()
	workspaceID := strings.TrimSpace(payload.WorktreeWorkspaceID)
	if workspaceID == "" {
		workspaceID = SuggestedWorktreeID(baseProject, botName)
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("无法推导 worktree workspace_id，请手动填写")
	}
	branchName := strings.TrimSpace(payload.WorktreeBranchName)
	if branchName == "" {
		branchName = SuggestedWorktreeBranch(baseProject, botName, "")
	}
	directoryName := strings.TrimSpace(payload.WorktreeDirectoryName)
	if directoryName == "" {
		directoryName = workspaceID
	}
	if strings.TrimSpace(payload.WorktreeWorkspaceID) == "" && strings.TrimSpace(payload.WorktreeBranchName) == "" && strings.TrimSpace(payload.WorktreeDirectoryName) == "" {
		branchName, workspaceID, directoryName = s.uniqueCloneWorktreeDefaults(parentDir, branchName, workspaceID, directoryName)
	}
	if err := validateGitBranchName(branchName); err != nil {
		return nil, fmt.Errorf("worktree 分支名无效: %w", err)
	}
	if err := validateWorktreeDirectoryName(directoryName); err != nil {
		return nil, err
	}
	targetDir := filepath.Join(parentDir, directoryName)
	if filepath.Clean(targetDir) == filepath.Clean(cloneTargetDir) {
		return nil, fmt.Errorf("worktree 目录不能和 clone 目录相同")
	}
	if existingWS := s.WorkspaceByCWD(targetDir); existingWS != nil {
		return nil, &CloneExistingWorkspaceError{WorkspaceID: existingWS.ID, TargetDir: targetDir}
	}
	if _, statErr := os.Stat(targetDir); statErr == nil {
		return nil, &CloneExistingDirError{WorkspaceID: workspaceID, TargetDir: targetDir}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if existing := config.FindWorkspace(s.App.Config(), workspaceID); existing != nil {
		if sameWorkspaceCWD(existing.Cwd, targetDir) {
			return nil, &CloneExistingWorkspaceError{WorkspaceID: existing.ID, TargetDir: targetDir}
		}
		return nil, fmt.Errorf("worktree workspace %q 已存在，请换一个 workspace_id", workspaceID)
	}
	return &CloneWorktreePlan{
		BaseRepoRoot:  cloneTargetDir,
		BranchName:    branchName,
		WorkspaceID:   workspaceID,
		DirectoryName: directoryName,
		TargetDir:     targetDir,
	}, nil
}

func (s *ManagementService) worktreeBotLabel() string {
	if s != nil && s.App != nil {
		if client := s.App.Feishu(); client != nil {
			if name := strings.TrimSpace(client.BotName()); name != "" {
				return name
			}
		}
		if frontendID := strings.TrimSpace(s.App.FrontendID()); frontendID != "" {
			return frontendID
		}
	}
	return "bot"
}

func (s *ManagementService) worktreeBaseProjectLabel(ws *config.Workspace) string {
	if ws == nil {
		return "workspace"
	}
	if root, err := gitRepoRoot(ws.Cwd); err == nil {
		if base := cleanPathBase(root); base != "" {
			return base
		}
	}
	if base := cleanPathBase(ws.Cwd); base != "" {
		return base
	}
	return appcore.FirstNonEmpty(strings.TrimSpace(ws.Name), strings.TrimSpace(ws.ID), "workspace")
}

func cleanPathBase(pathValue string) string {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(pathValue))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func (s *ManagementService) uniqueWorktreeDefaults(baseRepoRoot, branchName, workspaceID, directoryName string) (string, string, string) {
	parentDir := filepath.Dir(strings.TrimSpace(baseRepoRoot))
	for i := 1; i <= 100; i++ {
		candidateBranch := branchWithNumericSuffix(branchName, i)
		candidateID := withNumericSuffix(workspaceID, i)
		candidateDir := withNumericSuffix(directoryName, i)
		if s.worktreeDefaultAvailable(baseRepoRoot, parentDir, candidateBranch, candidateID, candidateDir, true) {
			return candidateBranch, candidateID, candidateDir
		}
	}
	return branchName, workspaceID, directoryName
}

func (s *ManagementService) uniqueCloneWorktreeDefaults(parentDir, branchName, workspaceID, directoryName string) (string, string, string) {
	for i := 1; i <= 100; i++ {
		candidateBranch := branchWithNumericSuffix(branchName, i)
		candidateID := withNumericSuffix(workspaceID, i)
		candidateDir := withNumericSuffix(directoryName, i)
		if s.worktreeDefaultAvailable("", parentDir, candidateBranch, candidateID, candidateDir, false) {
			return candidateBranch, candidateID, candidateDir
		}
	}
	return branchName, workspaceID, directoryName
}

func (s *ManagementService) worktreeDefaultAvailable(baseRepoRoot, parentDir, branchName, workspaceID, directoryName string, checkBranch bool) bool {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(directoryName) == "" || strings.TrimSpace(branchName) == "" {
		return false
	}
	if config.FindWorkspace(s.App.Config(), workspaceID) != nil {
		return false
	}
	targetDir := filepath.Join(parentDir, directoryName)
	if s.WorkspaceByCWD(targetDir) != nil {
		return false
	}
	if _, err := os.Stat(targetDir); err == nil || (err != nil && !errors.Is(err, os.ErrNotExist)) {
		return false
	}
	if s.pendingWorktreeDefaultReserved(workspaceID, targetDir, branchName) {
		return false
	}
	if checkBranch {
		if exists, err := gitBranchExists(baseRepoRoot, branchName); err == nil && exists {
			return false
		}
	}
	return true
}

func (s *ManagementService) pendingWorktreeDefaultReserved(workspaceID, targetDir, branchName string) bool {
	if s == nil || s.App == nil || s.App.Store() == nil {
		return false
	}
	workspaceID = strings.TrimSpace(workspaceID)
	targetDir = filepath.Clean(strings.TrimSpace(targetDir))
	branchName = strings.TrimSpace(branchName)
	for _, pending := range s.App.Store().AllPendingRequests() {
		if pending == nil {
			continue
		}
		status := state.NormalizePendingRequestStatus(pending.Status)
		if status == state.PendingRequestStatusResolved || status == state.PendingRequestStatusExpired {
			continue
		}
		switch strings.TrimSpace(pending.Kind) {
		case "workspace_worktree":
			payload := WorktreePayloadFromPending(pending)
			if worktreePayloadReserves(payload.WorkspaceID, payload.TargetDir, payload.BranchName, workspaceID, targetDir, branchName) {
				return true
			}
		case "workspace_clone":
			payload := ClonePayloadFromPending(pending)
			if !CloneCreatesWorktree(payload) {
				continue
			}
			if worktreePayloadReserves(payload.WorktreeWorkspaceID, payload.WorktreeTargetDir, payload.WorktreeBranchName, workspaceID, targetDir, branchName) {
				return true
			}
		}
	}
	return false
}

func worktreePayloadReserves(payloadWorkspaceID, payloadTargetDir, payloadBranchName, workspaceID, targetDir, branchName string) bool {
	if workspaceID != "" && strings.TrimSpace(payloadWorkspaceID) == workspaceID {
		return true
	}
	if branchName != "" && strings.TrimSpace(payloadBranchName) == branchName {
		return true
	}
	if strings.TrimSpace(payloadTargetDir) != "" && filepath.Clean(strings.TrimSpace(payloadTargetDir)) == targetDir {
		return true
	}
	return false
}

func withNumericSuffix(value string, index int) string {
	value = strings.TrimSpace(value)
	if index <= 1 || value == "" {
		return value
	}
	return fmt.Sprintf("%s-%d", value, index)
}

func branchWithNumericSuffix(branchName string, index int) string {
	branchName = strings.TrimSpace(branchName)
	if index <= 1 || branchName == "" {
		return branchName
	}
	idx := strings.LastIndex(branchName, "/")
	if idx < 0 {
		return withNumericSuffix(branchName, index)
	}
	prefix := strings.TrimRight(branchName[:idx+1], "/")
	leaf := branchName[idx+1:]
	if prefix == "" {
		return withNumericSuffix(leaf, index)
	}
	return prefix + "/" + withNumericSuffix(leaf, index)
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
	slog.Debug("workspace clone started",
		"request_id", requestID,
		"message_id", messageID,
		"session_key", sessionKey,
		"repo_url", payload.RepoURL,
		"parent_dir", parentDir,
	)
	workspaceID, targetDir, err := s.CloneWorkspacePayloadInParent(
		ctx,
		sessionKey,
		userID,
		chatID,
		chatType,
		payload,
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
				req.Status = state.PendingRequestStatusResolved.String()
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
				req.Status = state.PendingRequestStatusResolved.String()
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
			req.Status = state.PendingRequestStatusPending.String()
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
		req.Status = state.PendingRequestStatusResolved.String()
		req.PayloadJSON = appcore.MustJSON(payload)
	})
	if strings.TrimSpace(messageID) != "" {
		_ = s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderCloneSuccessCard(sessionKey, workspaceID, targetDir))
	}
}

// FinishWorkspaceWorktreeSubmit completes a worktree operation in the background.
func (s *ManagementService) FinishWorkspaceWorktreeSubmit(ctx context.Context, op *CloneOperation, requestID, messageID, sessionKey, userID, chatID, chatType string, payload WorktreePayload, plan *WorktreePlan) {
	defer s.ClearWorkspaceCloneOperation(requestID)
	if plan == nil {
		planErr := "worktree 创建参数无效，请重新发起。"
		payload.ErrorMessage = planErr
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusPending.String()
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		})
		if strings.TrimSpace(messageID) != "" {
			_ = s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderWorktreeCard(sessionKey, requestID, payload))
		}
		return
	}
	if err := s.GitWorktreeAdd(ctx, plan.BaseRepoRoot, plan.BranchName, plan.TargetDir); err != nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
				req.Status = state.PendingRequestStatusResolved.String()
				req.PayloadJSON = appcore.MustJSON(payload)
				req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
			})
			if strings.TrimSpace(messageID) != "" {
				_ = s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderWorktreeCanceledCard(sessionKey, payload, plan, op.Snapshot()))
			}
			return
		}
		payload.ErrorMessage = err.Error()
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusPending.String()
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		})
		if strings.TrimSpace(messageID) != "" {
			_ = s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderWorktreeCard(sessionKey, requestID, payload))
		}
		return
	}
	if err := s.CreateWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, plan.WorkspaceID, plan.WorkspaceID, plan.TargetDir); err != nil {
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusResolved.String()
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
		})
		if strings.TrimSpace(messageID) != "" {
			_ = s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderWorktreeManualHintCard(sessionKey, plan.WorkspaceID, plan.TargetDir, err.Error()))
		}
		return
	}
	_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
		req.PayloadJSON = appcore.MustJSON(payload)
	})
	if strings.TrimSpace(messageID) != "" {
		_ = s.App.Feishu().PatchCard(context.Background(), messageID, s.RenderWorktreeSuccessCard(sessionKey, plan.WorkspaceID, plan.TargetDir))
	}
}

// CompleteWorkspaceSandboxSet handles sandbox mode setting.
func (s *ManagementService) CompleteWorkspaceSandboxSet(action *feishu.CardAction, sessionKey, workspaceID, sandboxMode string) (*callback.CardActionTriggerResponse, error) {
	return appbackend.DriverForApp(s.App).Permission().CompleteWorkspaceSandboxSet(sessionKey, workspaceID, sandboxMode, appbackend.WorkspacePermissionUpdateDeps{
		UpdateWorkspaceDefaults: s.updateWorkspaceDefaults,
		RenderSandboxMenu:       s.renderSandboxMenuCard,
		RenderPolicyMenu:        s.renderPolicyMenuCard,
	})
}

// CompleteWorkspacePolicySet handles approval policy setting.
func (s *ManagementService) CompleteWorkspacePolicySet(action *feishu.CardAction, sessionKey, workspaceID, approvalPolicy string) (*callback.CardActionTriggerResponse, error) {
	return appbackend.DriverForApp(s.App).Permission().CompleteWorkspacePolicySet(sessionKey, workspaceID, approvalPolicy, appbackend.WorkspacePermissionUpdateDeps{
		UpdateWorkspaceDefaults: s.updateWorkspaceDefaults,
		RenderSandboxMenu:       s.renderSandboxMenuCard,
		RenderPolicyMenu:        s.renderPolicyMenuCard,
	})
}

// CompleteWorkspaceMultiAgentSet handles multi-agent mode setting.
func (s *ManagementService) CompleteWorkspaceMultiAgentSet(action *feishu.CardAction, sessionKey, workspaceID, mode string) (*callback.CardActionTriggerResponse, error) {
	return appbackend.DriverForApp(s.App).Permission().CompleteWorkspaceMultiAgentSet(sessionKey, workspaceID, mode, appbackend.WorkspacePermissionUpdateDeps{
		UpdateWorkspaceDefaults: s.updateWorkspaceDefaults,
		RenderSandboxMenu:       s.renderSandboxMenuCard,
		RenderPolicyMenu:        s.renderPolicyMenuCard,
		RenderMultiAgentMenu:    s.renderMultiAgentMenuCard,
	})
}

func (s *ManagementService) CompleteWorkspacePermissionModeSet(action *feishu.CardAction, sessionKey, workspaceID, rawMode string) (*callback.CardActionTriggerResponse, error) {
	return appbackend.DriverForApp(s.App).Permission().CompleteWorkspacePermissionModeSet(sessionKey, workspaceID, rawMode, appbackend.WorkspacePermissionModeUpdateDeps{
		App:                     s.App,
		Session:                 s.GetSession,
		UpdateWorkspaceDefaults: s.updateWorkspaceDefaults,
		ApplyRuntime:            func(sessionKey, mode string) error { return nil },
		RenderPermissionMenu: func(sessionKey string) (map[string]any, error) {
			return appbackend.DriverForApp(s.App).Permission().RenderWorkspacePermissionModeMenu(sessionKey, appbackend.WorkspacePermissionRenderDeps{
				App:            s.App,
				FormatMenuBody: s.FormatMenuBody,
			})
		},
	})
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
	if reason := workspaceSwitchBlockedReason(sess, s.SessionHasInFlight(sess)); reason != "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: reason}}, nil
	}
	if err := setSelectedWorkspaceForSession(s.App, sess, workspaceID); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if err := applyWorkspaceSwitch(s, sessionKey, sess, workspaceID); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	if ws != nil {
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
			s.OnAsyncDone()
			return
		}
		if reason := workspaceSwitchBlockedReason(sess, s.SessionHasInFlight(sess)); reason != "" {
			slog.Debug("workspace action thread binding skipped",
				"session_key", sessionKey,
				"workspace_id", workspaceID,
				"reason", reason,
			)
			s.OnAsyncDone()
			return
		}
		if strings.TrimSpace(sess.WorkspaceID) != strings.TrimSpace(workspaceID) {
			s.OnAsyncDone()
			return
		}
		if strings.TrimSpace(sess.ActiveThreadID) != "" && strings.TrimSpace(sess.ActiveThreadWorkspaceID) == strings.TrimSpace(workspaceID) {
			s.OnAsyncDone()
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
		s.OnAsyncDone()
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
		CloneMode:         CloneModeWorkspace,
	}
	if err := s.SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_clone",
		SessionKey:  sessionKey,
		OwnerUserID: action.UserID,
		FeishuMsgID: strings.TrimSpace(action.MessageID),
		PayloadJSON: appcore.MustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
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

// CompleteWorkspaceWorktree opens the worktree creation card from the menu.
func (s *ManagementService) CompleteWorkspaceWorktree(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	requestID, err := s.NextLocalID("workspace")
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	msg := s.CommandMessageFromAction(action, sessionKey, "/workspace new worktree")
	_, _, ws := s.currentWorkspaceForMessage(msg)
	payload := s.DefaultWorkspaceWorktreePayload(msg, ws, "", "")
	if err := s.SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        "workspace_worktree",
		SessionKey:  sessionKey,
		OwnerUserID: action.UserID,
		FeishuMsgID: strings.TrimSpace(action.MessageID),
		PayloadJSON: appcore.MustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	}); err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "已打开 Worktree 创建表单"},
		Card:  rawCard(s.RenderWorktreeCard(sessionKey, requestID, payload)),
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
	payload = s.DefaultCloneWorktreePayload(payload, currentPath)
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
		parentDir = appcore.FirstNonEmpty(strings.TrimSpace(s.DefaultWorkspaceCloneParent(ws)), "/")
	}
	payload.SelectedParentDir = parentDir
	payload = s.DefaultCloneWorktreePayload(payload, parentDir)
	_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusPending.String()
		req.PayloadJSON = appcore.MustJSON(payload)
		req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
	})
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
		snapshot := op.RequestCancel()
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusCancelling.String()
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
			req.Status = state.PendingRequestStatusResolved.String()
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
		req.Status = state.PendingRequestStatusResolved.String()
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
	defaultMessageChatType(msg)
	sessionKey, _, ws := s.currentWorkspaceForMessage(msg)
	parentDir := strings.TrimSpace(payload.SelectedParentDir)
	if parentDir == "" {
		parentDir = appcore.FirstNonEmpty(strings.TrimSpace(s.DefaultWorkspaceCloneParent(ws)), "/")
	}
	payload.SelectedParentDir = parentDir
	messageID := appcore.FirstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID))
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
	plan, err := s.PrepareWorkspaceClonePayload(payload, parentDir)
	if err != nil {
		var existingWorkspaceErr *CloneExistingWorkspaceError
		if errors.As(err, &existingWorkspaceErr) {
			_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
				req.Status = state.PendingRequestStatusResolved.String()
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
				req.Status = state.PendingRequestStatusResolved.String()
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
			req.Status = state.PendingRequestStatusPending.String()
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		})
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(s.RenderCloneCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	payload = clonePayloadWithPlan(payload, plan)
	ctx, cancel := context.WithCancel(context.Background())
	op := NewCloneOperation(cancel)
	s.SetWorkspaceCloneOperation(requestID, op)
	_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusProcessing.String()
		req.PayloadJSON = appcore.MustJSON(payload)
		req.FeishuMsgID = appcore.FirstNonEmpty(strings.TrimSpace(req.FeishuMsgID), messageID)
		req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
	})
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
		plan, _ := s.PrepareWorkspaceWorktree(payload)
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "info", Content: "正在创建 Worktree 工作区"},
			Card:  rawCard(s.RenderWorktreePreparingCard(requestID, payload, plan, snapshot)),
		}, nil
	}
	plan, err := s.PrepareWorkspaceWorktree(payload)
	if err != nil {
		var existingWorkspaceErr *CloneExistingWorkspaceError
		if errors.As(err, &existingWorkspaceErr) {
			_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
				req.Status = state.PendingRequestStatusResolved.String()
				req.PayloadJSON = appcore.MustJSON(payload)
				req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
			})
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{Type: "info", Content: "目标目录已经由现有工作区接管，可直接切换"},
				Card:  rawCard(s.RenderSwitchExistingCard(pending.SessionKey, existingWorkspaceErr.WorkspaceID, existingWorkspaceErr.TargetDir, "worktree 目标目录已经由现有工作区接管。")),
			}, nil
		}
		payload.ErrorMessage = err.Error()
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusPending.String()
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		})
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(s.RenderWorktreeCard(pending.SessionKey, requestID, payload)),
		}, nil
	}
	payload.BaseWorkspaceID = plan.BaseWorkspaceID
	payload.BranchName = plan.BranchName
	payload.WorkspaceID = plan.WorkspaceID
	payload.DirectoryName = plan.DirectoryName
	payload.TargetDir = plan.TargetDir
	messageID := appcore.FirstNonEmpty(strings.TrimSpace(pending.FeishuMsgID), strings.TrimSpace(action.MessageID))
	ctx, cancel := context.WithCancel(context.Background())
	op := NewCloneOperation(cancel)
	s.SetWorkspaceCloneOperation(requestID, op)
	_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusProcessing.String()
		req.PayloadJSON = appcore.MustJSON(payload)
		req.FeishuMsgID = appcore.FirstNonEmpty(strings.TrimSpace(req.FeishuMsgID), messageID)
		req.ExpiresAt = time.Now().Add(30 * time.Minute).Unix()
	})
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
	plan, _ := s.PrepareWorkspaceWorktree(payload)
	if op := s.GetWorkspaceCloneOperation(requestID); op != nil {
		snapshot := op.RequestCancel()
		_ = s.UpdatePending(requestID, func(req *state.PendingRequest) {
			req.Status = state.PendingRequestStatusCancelling.String()
			req.PayloadJSON = appcore.MustJSON(payload)
			req.ExpiresAt = time.Now().Add(10 * time.Minute).Unix()
		})
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
			req.Status = state.PendingRequestStatusResolved.String()
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
	_ = s.UpdatePending(pending.ID, func(req *state.PendingRequest) { req.Status = state.PendingRequestStatusResolved.String() })
	if pending.FeishuMsgID != "" {
		_ = s.App.Feishu().PatchCard(context.Background(), pending.FeishuMsgID, s.App.Feishu().SimpleStatusCard("工作区已创建", "green", "已创建并切换到工作区 `"+id+"`\n\ncwd: `"+cwd+"`", nil))
	}
	return s.App.Feishu().ReplyText(context.Background(), msg.MessageID, "已创建并切换到工作区 "+id, appcore.ReplyInThreadEnabled(s.App, msg.ChatType))
}

// --- private helpers ---

func (s *ManagementService) currentWorkspaceForMessage(msg *feishu.InboundMessage) (sessionKey string, sess *state.Session, ws *config.Workspace) {
	sessionKey = appcore.MakeSessionKey(s.App, msg)
	sess = s.GetSession(sessionKey)
	workspaceID := selectedWorkspaceIDForMessage(s.App, msg, sess)
	return sessionKey, sess, config.FindWorkspace(s.App.Config(), workspaceID)
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
	if s != nil && s.deps.Async.RunAsync != nil {
		s.deps.Async.RunAsync(fn)
		return
	}
	go fn()
}

func (s *ManagementService) CloneWorkspaceInParent(ctx context.Context, sessionKey, userID, chatID, chatType, repoURL, explicitID, parentDir string, report CloneProgressReporter) (string, string, error) {
	payload := ClonePayload{RepoURL: strings.TrimSpace(repoURL), DraftID: strings.TrimSpace(explicitID), CloneMode: CloneModeWorkspace}
	return s.CloneWorkspacePayloadInParent(ctx, sessionKey, userID, chatID, chatType, payload, parentDir, report)
}

func (s *ManagementService) CloneWorkspacePayloadInParent(ctx context.Context, sessionKey, userID, chatID, chatType string, payload ClonePayload, parentDir string, report CloneProgressReporter) (string, string, error) {
	plan, err := s.PrepareWorkspaceClonePayload(payload, parentDir)
	if err != nil {
		return "", "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(filepath.Dir(plan.TargetDir), 0o755); err != nil {
		return "", "", err
	}
	if err := s.GitClone(ctx, strings.TrimSpace(payload.RepoURL), plan.TargetDir, report); err != nil {
		return "", "", err
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	finalWorkspaceID := plan.WorkspaceID
	finalTargetDir := plan.TargetDir
	if plan.Worktree != nil {
		if err := s.GitWorktreeAdd(ctx, plan.Worktree.BaseRepoRoot, plan.Worktree.BranchName, plan.Worktree.TargetDir); err != nil {
			return "", "", err
		}
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		finalWorkspaceID = plan.Worktree.WorkspaceID
		finalTargetDir = plan.Worktree.TargetDir
	}
	if err := s.CreateWorkspaceAndSwitch(sessionKey, userID, chatID, chatType, finalWorkspaceID, finalWorkspaceID, finalTargetDir); err != nil {
		return "", "", &CloneTakeoverError{
			WorkspaceID: finalWorkspaceID,
			TargetDir:   finalTargetDir,
			Err:         err,
		}
	}
	return finalWorkspaceID, finalTargetDir, nil
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

func gitRepoRoot(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", fmt.Errorf("cwd is required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("未检测到 git: %w", err)
	}
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("git repo root is empty")
	}
	return filepath.Clean(root), nil
}

func validateGitBranchName(branchName string) error {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		return fmt.Errorf("branch name is required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return err
	}
	cmd := exec.Command("git", "check-ref-format", "--branch", branchName)
	if output, err := cmd.CombinedOutput(); err != nil {
		if text := strings.TrimSpace(string(output)); text != "" {
			return fmt.Errorf("%s", text)
		}
		return err
	}
	return nil
}

func gitBranchExists(repoRoot, branchName string) (bool, error) {
	cmd := exec.Command("git", "-C", strings.TrimSpace(repoRoot), "show-ref", "--verify", "--quiet", "refs/heads/"+strings.TrimSpace(branchName))
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func validateWorktreeDirectoryName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("请填写本地目录名")
	}
	if name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("本地目录名无效，请填写不含路径分隔符的普通目录名")
	}
	return nil
}

// renderSandboxMenuCard is a helper that re-renders the sandbox menu card.
func (s *ManagementService) renderSandboxMenuCard(sessionKey string) (map[string]any, error) {
	return appbackend.DriverForApp(s.App).Permission().RenderWorkspaceSandboxMenu(sessionKey, appbackend.WorkspacePermissionRenderDeps{
		App:            s.App,
		FormatMenuBody: s.FormatMenuBody,
	})
}

// renderPolicyMenuCard is a helper that re-renders the policy menu card.
func (s *ManagementService) renderPolicyMenuCard(sessionKey string) (map[string]any, error) {
	return appbackend.DriverForApp(s.App).Permission().RenderWorkspacePolicyMenu(sessionKey, appbackend.WorkspacePermissionRenderDeps{
		App:            s.App,
		FormatMenuBody: s.FormatMenuBody,
	})
}

// renderMultiAgentMenuCard is a helper that re-renders the multi-agent menu card.
func (s *ManagementService) renderMultiAgentMenuCard(sessionKey string) (map[string]any, error) {
	return appbackend.DriverForApp(s.App).Permission().RenderWorkspaceMultiAgentMenu(sessionKey, appbackend.WorkspacePermissionRenderDeps{
		App:            s.App,
		FormatMenuBody: s.FormatMenuBody,
	})
}
