package workspace

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"feidex/internal/state"
)

const (
	PathPickerKind          = "path_picker"
	PathPickerModeDirectory = "directory"
	PathPickerModeFile      = "file"
	PathPickerStyleDropdown = "dropdown"
	CloneModeWorkspace      = "workspace"
	CloneModeWorktree       = "worktree"

	CommandUsage = "/workspace | /workspace list | /workspace new | /workspace new worktree [BRANCH] [ID] | /workspace clone GIT_URL [ID] [--parent DIR] | /workspace use ID | /workspace delete [ID] | /workspace sandbox [MODE] | /workspace policy [POLICY]"
)

type PathPickerPayload struct {
	Mode         string `json:"mode"`
	Style        string `json:"style"`
	RootPath     string `json:"root_path"`
	CurrentPath  string `json:"current_path"`
	SelectedPath string `json:"selected_path,omitempty"`
}

type PathPickerEntry struct {
	Name  string
	Path  string
	IsDir bool
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

type CloneTakeoverError struct {
	WorkspaceID string
	TargetDir   string
	Err         error
}

type CloneExistingDirError struct {
	WorkspaceID string
	TargetDir   string
}

type CloneExistingWorkspaceError struct {
	WorkspaceID string
	TargetDir   string
}

type CloneProgressSnapshot struct {
	StartedAt      time.Time
	LastProgressAt time.Time
	State          string
	Lines          []string
}

type ClonePlan struct {
	RepoName    string
	WorkspaceID string
	TargetDir   string
	Worktree    *CloneWorktreePlan
}

type CloneWorktreePlan struct {
	BaseRepoRoot  string
	BranchName    string
	WorkspaceID   string
	DirectoryName string
	TargetDir     string
}

type WorktreePlan struct {
	BaseWorkspaceID string
	BaseRepoRoot    string
	BranchName      string
	WorkspaceID     string
	DirectoryName   string
	TargetDir       string
}

func (e *CloneTakeoverError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("仓库已拉取到 %q，但创建工作区失败: %v", e.TargetDir, e.Err)
}

func (e *CloneTakeoverError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *CloneExistingDirError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("目标目录已存在: %s", e.TargetDir)
}

func (e *CloneExistingWorkspaceError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("目标目录 %q 已由工作区 %q 接管", e.TargetDir, e.WorkspaceID)
}

// SettingOption represents a workspace setting choice (sandbox mode or approval policy).
type SettingOption struct {
	Value string
	Label string
}

// ThreadBinding represents the result of binding a session to a workspace thread.
type ThreadBinding struct {
	ThreadID string
	Name     string
	Preview  string
	Resumed  bool
}

// SandboxOptions returns the available sandbox mode options.
func SandboxOptions() []SettingOption {
	return []SettingOption{
		{Value: "read-only", Label: "read-only"},
		{Value: "workspace-write", Label: "workspace-write"},
		{Value: "danger-full-access", Label: "danger-full-access"},
	}
}

// ApprovalPolicyOptions returns the available approval policy options.
func ApprovalPolicyOptions() []SettingOption {
	return []SettingOption{
		{Value: "untrusted", Label: "untrusted"},
		{Value: "on-request", Label: "on-request"},
		{Value: "never", Label: "never"},
	}
}

// MultiAgentModeOptions returns the available multi-agent mode options.
func MultiAgentModeOptions() []SettingOption {
	return []SettingOption{
		{Value: "explicitRequestOnly", Label: "explicit request only"},
		{Value: "proactive", Label: "proactive"},
		{Value: "none", Label: "none"},
	}
}

// ParseCloneArgs parses /workspace clone arguments into repo URL, workspace ID, and parent dir.
func ParseCloneArgs(args []string) (repoURL, workspaceID, parentDir string, err error) {
	if len(args) < 2 || strings.TrimSpace(args[0]) != "clone" {
		return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
	}
	repoURL = strings.TrimSpace(args[1])
	if repoURL == "" {
		return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
	}
	switch len(args) {
	case 2:
		return repoURL, "", "", nil
	case 3:
		if strings.TrimSpace(args[2]) == "--parent" {
			return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
		}
		return repoURL, strings.TrimSpace(args[2]), "", nil
	case 4:
		if strings.TrimSpace(args[2]) != "--parent" || strings.TrimSpace(args[3]) == "" {
			return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
		}
		return repoURL, "", strings.TrimSpace(args[3]), nil
	case 5:
		if strings.TrimSpace(args[2]) == "" || strings.TrimSpace(args[3]) != "--parent" || strings.TrimSpace(args[4]) == "" {
			return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
		}
		return repoURL, strings.TrimSpace(args[2]), strings.TrimSpace(args[4]), nil
	default:
		return "", "", "", fmt.Errorf("usage: %s", CommandUsage)
	}
}

// NewPayloadFromPending extracts a NewPayload from a pending request.
func NewPayloadFromPending(pending *state.PendingRequest) NewPayload {
	var payload NewPayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

// ClonePayloadFromPending extracts a ClonePayload from a pending request.
func ClonePayloadFromPending(pending *state.PendingRequest) ClonePayload {
	var payload ClonePayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}
