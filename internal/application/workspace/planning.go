package workspace

import (
	"encoding/json"
	"errors"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	domain "feidex/internal/domain/workspace"
	"fmt"
	"path/filepath"
	"strings"
)

var ErrPathNotExist = errors.New("path does not exist")

type PlanningPaths interface{ Stat(string) (bool, error) }
type PlanningGit interface {
	Root(string) (string, error)
	ValidateBranch(string) error
	BranchExists(string, string) (bool, error)
}
type PlanningService struct {
	Repository                      ConfigurationRepository
	PendingRequests                 func() []*interaction.PendingRequest
	ConfigPath, BotName, FrontendID func() string
	Paths                           PlanningPaths
	Git                             PlanningGit
}

func (s *PlanningService) WorkspaceByID(id string) *domain.Workspace {
	for _, value := range s.Repository.List() {
		if value.ID == strings.TrimSpace(id) {
			return &value
		}
	}
	return nil
}
func worktreePayloadFromPending(p *interaction.PendingRequest) (v WorktreePayload) {
	_ = json.Unmarshal([]byte(p.PayloadJSON), &v)
	return
}
func clonePayloadFromPending(p *interaction.PendingRequest) (v ClonePayload) {
	_ = json.Unmarshal([]byte(p.PayloadJSON), &v)
	return
}

func (s *PlanningService) DefaultWorkspaceWorktreePayload(ws *domain.Workspace, branchName, workspaceID string) WorktreePayload {
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
	if repoRoot, err := s.Git.Root(func() string {
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

func (s *PlanningService) DefaultWorkspaceCloneRoot(ws *domain.Workspace) string {
	return "/"
}

func (s *PlanningService) DefaultWorkspaceCloneParent(ws *domain.Workspace) string {
	if ws != nil && strings.TrimSpace(ws.Cwd) != "" {
		return filepath.Dir(strings.TrimSpace(ws.Cwd))
	}
	if strings.TrimSpace(s.ConfigPath()) != "" {
		return filepath.Dir(strings.TrimSpace(s.ConfigPath()))
	}
	return "."
}

func (s *PlanningService) WorkspaceByCWD(targetDir string) *domain.Workspace {
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" {
		return nil
	}
	cleanTarget := filepath.Clean(targetDir)
	workspaces := s.Repository.List()
	for i := range workspaces {
		ws := &workspaces[i]
		if filepath.Clean(strings.TrimSpace(ws.Cwd)) == cleanTarget {
			return ws
		}
	}
	return nil
}

func (s *PlanningService) WorkspaceByIDAndCWD(workspaceID, targetDir string) *domain.Workspace {
	ws := s.WorkspaceByID(strings.TrimSpace(workspaceID))
	if ws == nil || !conversation.SameWorkspaceCWD(targetDir, ws.Cwd) {
		return nil
	}
	return ws
}

func (s *PlanningService) PrepareWorkspaceClone(repoURL, explicitID, parentDir string) (*ClonePlan, error) {
	payload := ClonePayload{RepoURL: strings.TrimSpace(repoURL), DraftID: strings.TrimSpace(explicitID), CloneMode: CloneModeWorkspace}
	return s.PrepareWorkspaceClonePayload(payload, parentDir)
}

func (s *PlanningService) PrepareWorkspaceClonePayload(payload ClonePayload, parentDir string) (*ClonePlan, error) {
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
	if _, statErr := s.Paths.Stat(targetDir); statErr == nil {
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
	} else if !errors.Is(statErr, ErrPathNotExist) {
		return nil, statErr
	}
	if !CloneCreatesWorktree(payload) && s.WorkspaceByID(workspaceID) != nil {
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

func (s *PlanningService) PrepareWorkspaceWorktree(payload WorktreePayload) (*WorktreePlan, error) {
	baseWorkspaceID := strings.TrimSpace(payload.BaseWorkspaceID)
	if baseWorkspaceID == "" {
		return nil, fmt.Errorf("请先选择基准工作区")
	}
	baseWS := s.WorkspaceByID(baseWorkspaceID)
	if baseWS == nil {
		return nil, fmt.Errorf("基准工作区 %q 不存在", baseWorkspaceID)
	}
	baseRepoRoot, err := s.Git.Root(strings.TrimSpace(baseWS.Cwd))
	if err != nil {
		return nil, fmt.Errorf("基准工作区不是可用的 Git 目录: %w", err)
	}
	branchName := strings.TrimSpace(payload.BranchName)
	if branchName == "" {
		return nil, fmt.Errorf("请填写新分支名")
	}
	if err := s.Git.ValidateBranch(branchName); err != nil {
		return nil, fmt.Errorf("分支名无效: %w", err)
	}
	if exists, err := s.Git.BranchExists(baseRepoRoot, branchName); err != nil {
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
	if _, statErr := s.Paths.Stat(targetDir); statErr == nil {
		return nil, &CloneExistingDirError{WorkspaceID: workspaceID, TargetDir: targetDir}
	} else if !errors.Is(statErr, ErrPathNotExist) {
		return nil, statErr
	}
	if existing := s.WorkspaceByID(workspaceID); existing != nil {
		if conversation.SameWorkspaceCWD(existing.Cwd, targetDir) {
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

func (s *PlanningService) ClonePayloadWithPlan(payload ClonePayload, plan *ClonePlan) ClonePayload {
	return clonePayloadWithPlan(payload, plan)
}

func (s *PlanningService) DefaultCloneWorktreePayload(payload ClonePayload, parentDir string) ClonePayload {
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
	baseProject := firstNonEmpty(strings.TrimSpace(repoName), "workspace")
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

func (s *PlanningService) prepareCloneWorktreePlan(payload ClonePayload, repoName, parentDir, cloneTargetDir string) (*CloneWorktreePlan, error) {
	baseProject := firstNonEmpty(strings.TrimSpace(repoName), "workspace")
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
	if err := s.Git.ValidateBranch(branchName); err != nil {
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
	if _, statErr := s.Paths.Stat(targetDir); statErr == nil {
		return nil, &CloneExistingDirError{WorkspaceID: workspaceID, TargetDir: targetDir}
	} else if !errors.Is(statErr, ErrPathNotExist) {
		return nil, statErr
	}
	if existing := s.WorkspaceByID(workspaceID); existing != nil {
		if conversation.SameWorkspaceCWD(existing.Cwd, targetDir) {
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

func (s *PlanningService) worktreeBotLabel() string {
	if s != nil && s.Repository != nil {
		if name := strings.TrimSpace(s.BotName()); name != "" {
			return name
		}
		if frontendID := strings.TrimSpace(s.FrontendID()); frontendID != "" {
			return frontendID
		}
	}
	return "bot"
}

func (s *PlanningService) worktreeBaseProjectLabel(ws *domain.Workspace) string {
	if ws == nil {
		return "workspace"
	}
	if root, err := s.Git.Root(ws.Cwd); err == nil {
		if base := cleanPathBase(root); base != "" {
			return base
		}
	}
	if base := cleanPathBase(ws.Cwd); base != "" {
		return base
	}
	return firstNonEmpty(strings.TrimSpace(ws.Name), strings.TrimSpace(ws.ID), "workspace")
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

func (s *PlanningService) uniqueWorktreeDefaults(baseRepoRoot, branchName, workspaceID, directoryName string) (string, string, string) {
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

func (s *PlanningService) uniqueCloneWorktreeDefaults(parentDir, branchName, workspaceID, directoryName string) (string, string, string) {
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

func (s *PlanningService) worktreeDefaultAvailable(baseRepoRoot, parentDir, branchName, workspaceID, directoryName string, checkBranch bool) bool {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(directoryName) == "" || strings.TrimSpace(branchName) == "" {
		return false
	}
	if s.WorkspaceByID(workspaceID) != nil {
		return false
	}
	targetDir := filepath.Join(parentDir, directoryName)
	if s.WorkspaceByCWD(targetDir) != nil {
		return false
	}
	if _, err := s.Paths.Stat(targetDir); err == nil || (err != nil && !errors.Is(err, ErrPathNotExist)) {
		return false
	}
	if s.pendingWorktreeDefaultReserved(workspaceID, targetDir, branchName) {
		return false
	}
	if checkBranch {
		if exists, err := s.Git.BranchExists(baseRepoRoot, branchName); err == nil && exists {
			return false
		}
	}
	return true
}

func (s *PlanningService) pendingWorktreeDefaultReserved(workspaceID, targetDir, branchName string) bool {
	if s.PendingRequests == nil {
		return false
	}
	workspaceID = strings.TrimSpace(workspaceID)
	targetDir = filepath.Clean(strings.TrimSpace(targetDir))
	branchName = strings.TrimSpace(branchName)
	for _, pending := range s.PendingRequests() {
		if pending == nil {
			continue
		}
		status := strings.TrimSpace(pending.Status)
		if status == "resolved" || status == "expired" {
			continue
		}
		switch strings.TrimSpace(pending.Kind) {
		case "workspace_worktree":
			payload := worktreePayloadFromPending(pending)
			if worktreePayloadReserves(payload.WorkspaceID, payload.TargetDir, payload.BranchName, workspaceID, targetDir, branchName) {
				return true
			}
		case "workspace_clone":
			payload := clonePayloadFromPending(pending)
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
