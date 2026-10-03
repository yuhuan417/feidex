package workspace

import (
	"fmt"

	"feidex/internal/domain/conversation"
	domain "feidex/internal/domain/workspace"
)

type (
	SettingOption         = domain.SettingOption
	PathPickerPayload     = domain.PathPickerPayload
	PathPickerEntry       = domain.PathPickerEntry
	NewPayload            = domain.NewPayload
	ClonePayload          = domain.ClonePayload
	WorktreePayload       = domain.WorktreePayload
	CloneProgressSnapshot = domain.CloneProgressSnapshot
	ClonePlan             = domain.ClonePlan
	CloneWorktreePlan     = domain.CloneWorktreePlan
	WorktreePlan          = domain.WorktreePlan
	ThreadBinding         = conversation.ThreadBinding
)

type CloneTakeoverError struct {
	WorkspaceID string
	TargetDir   string
	Err         error
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

type CloneExistingDirError struct {
	WorkspaceID string
	TargetDir   string
}

func (e *CloneExistingDirError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("目标目录已存在: %s", e.TargetDir)
}

type CloneExistingWorkspaceError struct {
	WorkspaceID string
	TargetDir   string
}

func (e *CloneExistingWorkspaceError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("目标目录 %q 已由工作区 %q 接管", e.TargetDir, e.WorkspaceID)
}

const (
	PathPickerKind          = domain.PathPickerKind
	PathPickerModeDirectory = domain.PathPickerModeDirectory
	PathPickerModeFile      = domain.PathPickerModeFile
	PathPickerStyleDropdown = domain.PathPickerStyleDropdown
	CloneModeWorkspace      = domain.CloneModeWorkspace
	CloneModeWorktree       = domain.CloneModeWorktree
)

func SandboxOptions() []SettingOption        { return domain.SandboxOptions() }
func ApprovalPolicyOptions() []SettingOption { return domain.ApprovalPolicyOptions() }
func MultiAgentModeOptions() []SettingOption { return domain.MultiAgentModeOptions() }
