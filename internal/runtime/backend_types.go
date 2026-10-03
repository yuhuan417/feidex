package runtime

import "feidex/internal/domain/backend"

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
type BackendKey = backend.Key

const (
	BackendKeyCodex  = backend.KeyCodex
	BackendKeyClaude = backend.KeyClaude
)

// BackendUpgradeSnapshot captures the state of a backend upgrade operation.
type BackendUpgradeSnapshot = backend.UpgradeSnapshot

// BackendRestartSnapshot captures the state of a backend restart operation.
type BackendRestartSnapshot = backend.RestartSnapshot
