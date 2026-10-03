package backend

import (
	"strings"

	appruntime "feidex/internal/runtime"
)

// BackendKey identifies a backend runtime.
type BackendKey = appruntime.BackendKey

const (
	BackendKeyCodex  = appruntime.BackendKeyCodex
	BackendKeyClaude = appruntime.BackendKeyClaude
)

// BackendUpgradeSnapshot is the upgrade state for a backend.
type BackendUpgradeSnapshot = appruntime.BackendUpgradeSnapshot

// BackendRestartSnapshot is the restart state for a backend.
type BackendRestartSnapshot = appruntime.BackendRestartSnapshot

// AllowsCommand reports whether a command is allowed during maintenance.
func AllowsCommand(raw string, commandName string) bool {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "/help":
		return true
	case raw == "/status":
		return true
	case raw == commandName:
		return true
	case strings.HasPrefix(raw, commandName+" "):
		return true
	default:
		return false
	}
}

// BlocksCommand returns an error if the command is blocked by active maintenance.
func BlocksCommand(t *appruntime.MaintenanceTracker, raw string, commandName string, displayName string) error {
	if t == nil || !t.Active() {
		return nil
	}
	if AllowsCommand(raw, commandName) {
		return nil
	}
	return ErrString(displayName + " 正在维护中，当前只允许 `" + commandName + "`、`/status`、`/help`")
}

// ErrString is a string that implements the error interface.
type ErrString string

func (e ErrString) Error() string { return string(e) }
