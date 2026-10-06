package workspace

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"feidex/internal/domain/conversation"
	domain "feidex/internal/domain/workspace"
	"feidex/internal/textutil"
)

func CloneDefaultID(repoName string) string { return SuggestedID(repoName) }

func CloneRepoName(repoURL string) (string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", fmt.Errorf("git 地址不能为空")
	}
	pathPart := repoURL
	if !strings.Contains(repoURL, "://") {
		if idx := strings.Index(repoURL, ":"); idx > 0 && !strings.Contains(repoURL[:idx], "/") && strings.Contains(repoURL[idx+1:], "/") {
			pathPart = repoURL[idx+1:]
		}
	} else if parsed, err := url.Parse(repoURL); err == nil {
		pathPart = firstNonEmpty(strings.TrimSpace(parsed.Path), strings.TrimSpace(parsed.Opaque))
	}
	base := strings.TrimSpace(strings.TrimSuffix(path.Base(strings.TrimSuffix(pathPart, "/")), ".git"))
	if base == "" || base == "." || base == "/" {
		return "", fmt.Errorf("无法从 git 地址推导仓库名")
	}
	return base, nil
}

func MergeNewFormValues(payload NewPayload, values map[string]string) NewPayload {
	if value, ok := values["workspace_id"]; ok {
		if value != "" {
			payload.DraftID = value
			if strings.TrimSpace(payload.AutoDraftID) != value {
				payload.AutoDraftID = ""
			}
		} else if strings.TrimSpace(payload.DraftID) == strings.TrimSpace(payload.AutoDraftID) {
			payload.DraftID, payload.AutoDraftID = "", ""
		}
	}
	if value, ok := values["workspace_name"]; ok {
		payload.DraftName = value
	}
	return payload
}

func MergeCloneFormValues(payload ClonePayload, values map[string]string) ClonePayload {
	if value, ok := values["repo_url"]; ok {
		payload.RepoURL = value
	}
	if value, ok := values["workspace_id"]; ok {
		payload.DraftID = value
	}
	if value, ok := values["clone_mode"]; ok {
		payload.CloneMode = NormalizeCloneMode(value)
	}
	if value, ok := values["worktree_branch_name"]; ok {
		payload.WorktreeBranchName = value
	}
	if value, ok := values["worktree_workspace_id"]; ok {
		payload.WorktreeWorkspaceID = value
	}
	if value, ok := values["worktree_directory_name"]; ok {
		payload.WorktreeDirectoryName = value
	}
	return payload
}

func NormalizeCloneMode(value string) string { return domain.NormalizeCloneMode(value) }
func CloneCreatesWorktree(payload ClonePayload) bool {
	return NormalizeCloneMode(payload.CloneMode) == CloneModeWorktree
}

func MergeWorktreeFormValues(payload WorktreePayload, values map[string]string) WorktreePayload {
	if value, ok := values["base_workspace_id"]; ok {
		payload.BaseWorkspaceID = value
	}
	if value, ok := values["branch_name"]; ok {
		payload.BranchName = value
	}
	if value, ok := values["workspace_id"]; ok {
		payload.WorkspaceID = value
	}
	if value, ok := values["directory_name"]; ok {
		payload.DirectoryName = value
	}
	return payload
}

func SuggestedWorktreeID(baseProject, botName string) string {
	base, bot := SuggestedID(baseProject), SuggestedID(botName)
	if base == "" {
		base = "workspace"
	}
	if bot == "" {
		bot = "bot"
	}
	return base + "-" + bot
}

func SuggestedWorktreeBranch(baseProject, botName, workspaceID string) string {
	project, bot := SuggestedID(baseProject), SuggestedID(botName)
	if project == "" {
		project = "workspace"
	}
	if bot == "" {
		bot = "bot"
	}
	parts := []string{"work", project, bot}
	if workspaceID = SuggestedID(workspaceID); workspaceID != "" {
		parts = append(parts, workspaceID)
	}
	return strings.Join(parts, "/")
}

func NewTakeoverNotice(targetDir string) string {
	return "clone 目标目录已存在，可直接新建工作区接管。\n\n目录已预填为 `" + firstNonEmpty(strings.TrimSpace(targetDir), "-") + "`，并已带上建议的 `workspace_id`。"
}
func NewExistingWorkspaceNotice() string {
	return "该 workspace_id 已存在，并且目录与现有工作区一致。"
}
func NewTakeoverPayload(workspaceID, targetDir string) NewPayload {
	return NewTakeoverPayloadWithNotice(workspaceID, targetDir, NewTakeoverNotice(targetDir))
}
func NewTakeoverPayloadWithNotice(workspaceID, targetDir, notice string) NewPayload {
	targetDir = strings.TrimSpace(targetDir)
	suggestedID := firstNonEmpty(strings.TrimSpace(workspaceID), SuggestedIDFromDir(targetDir))
	return NewPayload{RootPath: "/", SelectedCWD: targetDir, DraftID: suggestedID, AutoDraftID: suggestedID, Notice: strings.TrimSpace(notice)}
}
func SessionReferencesWorkspace(sess *conversation.Session, workspaceID string) bool {
	if sess == nil {
		return false
	}
	workspaceID = strings.TrimSpace(workspaceID)
	return strings.TrimSpace(sess.WorkspaceID) == workspaceID || strings.TrimSpace(sess.ActiveThreadWorkspaceID) == workspaceID
}

func firstNonEmpty(values ...string) string {
	return textutil.FirstNonEmpty(values...)
}
