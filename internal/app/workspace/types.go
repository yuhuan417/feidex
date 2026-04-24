package workspace

import (
	"fmt"
	"time"
)

const (
	PathPickerKind          = "path_picker"
	PathPickerModeDirectory = "directory"
	PathPickerModeFile      = "file"
	PathPickerStyleDropdown = "dropdown"
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
	RootPath          string             `json:"root_path"`
	SelectedParentDir string             `json:"selected_parent_dir,omitempty"`
	RepoURL           string             `json:"repo_url,omitempty"`
	DraftID           string             `json:"draft_id,omitempty"`
	ErrorMessage      string             `json:"error_message,omitempty"`
	Picker            *PathPickerPayload `json:"picker,omitempty"`
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
