package runtime

import "time"

// BackendUpgradePendingPayload describes a pending backend self-upgrade.
type BackendUpgradePendingPayload struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Command        string `json:"command"`
	CommandPath    string `json:"command_path"`
	UpdateCommand  string `json:"update_command"`
}

// ClaudeSessionListMeta holds metadata for a Claude session list entry.
type ClaudeSessionListMeta struct {
	ID        string
	Cwd       string
	Title     string
	Preview   string
	UpdatedAt time.Time
}

// ClaudeModelOption represents a selectable Claude model choice.
type ClaudeModelOption struct {
	Value string
	Label string
}

// ClaudePermissionModeOption represents a selectable Claude permission mode.
type ClaudePermissionModeOption struct {
	Value string
	Label string
}
