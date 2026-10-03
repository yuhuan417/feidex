package workspace

import (
	"encoding/json"
	"feidex/internal/domain/conversation"
	domain "feidex/internal/domain/workspace"
	"fmt"
	"strings"

	"feidex/internal/state"
)

const (
	PathPickerKind          = domain.PathPickerKind
	PathPickerModeDirectory = domain.PathPickerModeDirectory
	PathPickerModeFile      = domain.PathPickerModeFile
	PathPickerStyleDropdown = domain.PathPickerStyleDropdown
	CloneModeWorkspace      = domain.CloneModeWorkspace
	CloneModeWorktree       = domain.CloneModeWorktree

	CommandUsage = "/workspace | /workspace list | /workspace new | /workspace new worktree [BRANCH] [ID] | /workspace clone GIT_URL [ID] [--parent DIR] | /workspace use ID | /workspace delete [ID] | /workspace sandbox [MODE] | /workspace policy [POLICY]"
)

type PathPickerPayload = domain.PathPickerPayload
type PathPickerEntry = domain.PathPickerEntry
type NewPayload = domain.NewPayload
type ClonePayload = domain.ClonePayload
type WorktreePayload = domain.WorktreePayload

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

type CloneProgressSnapshot = domain.CloneProgressSnapshot
type ClonePlan = domain.ClonePlan
type CloneWorktreePlan = domain.CloneWorktreePlan
type WorktreePlan = domain.WorktreePlan

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
type SettingOption = domain.SettingOption

// ThreadBinding represents the result of binding a session to a workspace thread.
type ThreadBinding = conversation.ThreadBinding

// SandboxOptions returns the available sandbox mode options.
func SandboxOptions() []SettingOption {
	return domain.SandboxOptions()
}

// ApprovalPolicyOptions returns the available approval policy options.
func ApprovalPolicyOptions() []SettingOption {
	return domain.ApprovalPolicyOptions()
}

// MultiAgentModeOptions returns the available multi-agent mode options.
func MultiAgentModeOptions() []SettingOption {
	return domain.MultiAgentModeOptions()
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
