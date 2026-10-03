package runtime

import (
	"feidex/internal/domain/backend"
	"time"
)

// BackendUpgradePendingPayload describes a pending backend self-upgrade.
type BackendUpgradePendingPayload = backend.UpgradePayload

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
