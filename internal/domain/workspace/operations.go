package workspace

import (
	"strings"
	"time"
)

func NormalizeCloneMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), CloneModeWorktree) {
		return CloneModeWorktree
	}
	return CloneModeWorkspace
}

const (
	PathPickerKind          = "path_picker"
	PathPickerModeDirectory = "directory"
	PathPickerModeFile      = "file"
	PathPickerStyleDropdown = "dropdown"
	CloneModeWorkspace      = "workspace"
	CloneModeWorktree       = "worktree"
)

type PathPickerPayload struct {
	Mode         string `json:"mode"`
	Style        string `json:"style"`
	RootPath     string `json:"root_path"`
	CurrentPath  string `json:"current_path"`
	SelectedPath string `json:"selected_path,omitempty"`
}
type PathPickerEntry struct {
	Name, Path string
	IsDir      bool
}
type NewPayload struct {
	RootPath    string             `json:"root_path"`
	SelectedCWD string             `json:"selected_cwd"`
	DraftID     string             `json:"draft_id,omitempty"`
	AutoDraftID string             `json:"auto_draft_id,omitempty"`
	DraftName   string             `json:"draft_name,omitempty"`
	Notice      string             `json:"notice,omitempty"`
	Picker      *PathPickerPayload `json:"picker,omitempty"`
}
type ClonePayload struct {
	RootPath              string             `json:"root_path"`
	SelectedParentDir     string             `json:"selected_parent_dir,omitempty"`
	RepoURL               string             `json:"repo_url,omitempty"`
	DraftID               string             `json:"draft_id,omitempty"`
	CloneMode             string             `json:"clone_mode,omitempty"`
	WorktreeBranchName    string             `json:"worktree_branch_name,omitempty"`
	WorktreeWorkspaceID   string             `json:"worktree_workspace_id,omitempty"`
	WorktreeDirectoryName string             `json:"worktree_directory_name,omitempty"`
	WorktreeTargetDir     string             `json:"worktree_target_dir,omitempty"`
	ErrorMessage          string             `json:"error_message,omitempty"`
	Picker                *PathPickerPayload `json:"picker,omitempty"`
}
type WorktreePayload struct {
	BaseWorkspaceID string `json:"base_workspace_id,omitempty"`
	BranchName      string `json:"branch_name,omitempty"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
	DirectoryName   string `json:"directory_name,omitempty"`
	TargetDir       string `json:"target_dir,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
}
type CloneProgressSnapshot struct {
	StartedAt, LastProgressAt time.Time
	State                     string
	Lines                     []string
}
type ClonePlan struct {
	RepoName, WorkspaceID, TargetDir string
	Worktree                         *CloneWorktreePlan
}
type CloneWorktreePlan struct {
	BaseRepoRoot, BranchName, WorkspaceID, DirectoryName, TargetDir string
}
type WorktreePlan struct {
	BaseWorkspaceID, BaseRepoRoot, BranchName, WorkspaceID, DirectoryName, TargetDir string
}
