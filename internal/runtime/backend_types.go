package runtime

import "time"

// ClaudePermissionMode represents the permission mode for Claude sessions.
type ClaudePermissionMode string

const (
	ClaudePermissionModeDefault     ClaudePermissionMode = "default"
	ClaudePermissionModeAcceptEdits ClaudePermissionMode = "acceptEdits"
	ClaudePermissionModePlan        ClaudePermissionMode = "plan"
	ClaudePermissionModeBypass      ClaudePermissionMode = "bypassPermissions"
)

// ClaudeApprovalResolution describes how a Claude approval request should be resolved.
type ClaudeApprovalResolution struct {
	Behavior           string
	Scope              string
	Message            string
	Interrupt          bool
	UpdatedPermissions []map[string]any
}

// BackendKey identifies a backend for maintenance tracking.
type BackendKey string

const (
	BackendKeyCodex  BackendKey = "codex"
	BackendKeyClaude BackendKey = "claude"
)

// BackendUpgradeSnapshot captures the state of a backend upgrade operation.
type BackendUpgradeSnapshot struct {
	Running         bool
	Phase           string
	Result          string
	Message         string
	CurrentVersion  string
	PreviousVersion string
	TargetVersion   string
	LatestVersion   string
	StartedAt       time.Time
	UpdatedAt       time.Time
}

// BackendRestartSnapshot captures the state of a backend restart operation.
type BackendRestartSnapshot struct {
	Running        bool
	Phase          string
	Result         string
	Message        string
	CurrentVersion string
	StartedAt      time.Time
	UpdatedAt      time.Time
}
