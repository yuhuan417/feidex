package backend

import (
	"strings"
	"time"

	"feidex/internal/app/appcore"
	applifecycle "feidex/internal/app/lifecycle"
	appsessionctx "feidex/internal/app/sessionctx"
	"feidex/internal/state"
)

// Now is a testable clock.
var Now = time.Now

const sessionStatusCompacting = "compacting"

// SessionHasActiveWork reports whether a session has active work.
func SessionHasActiveWork(sess *state.Session) bool {
	if sess == nil {
		return false
	}
	if appsessionctx.HasActiveOperations(sess) {
		return true
	}
	switch strings.TrimSpace(sess.Status) {
	case sessionStatusCompacting, "turn_starting":
		return true
	default:
		return false
	}
}

// MaintenanceStateService provides maintenance tracker operations.
type MaintenanceStateService struct {
	App App
}

// NewMaintenanceStateService creates a new service.
func NewMaintenanceStateService(app App) MaintenanceStateService {
	return MaintenanceStateService{App: app}
}

// MaintenanceTracker returns the tracker for the given key.
func (s MaintenanceStateService) MaintenanceTracker(key BackendKey) *MaintenanceTracker {
	if s.App == nil {
		return nil
	}
	trackers := s.App.MaintenanceTrackers()
	if trackers == nil {
		return nil
	}
	return trackers.GetOrCreate(key)
}

// UpgradeState returns a snapshot of the upgrade state.
func (s MaintenanceStateService) UpgradeState(key BackendKey) BackendUpgradeSnapshot {
	if s.App == nil {
		return BackendUpgradeSnapshot{}
	}
	return s.MaintenanceTracker(key).UpgradeState()
}

// RestartState returns a snapshot of the restart state.
func (s MaintenanceStateService) RestartState(key BackendKey) BackendRestartSnapshot {
	if s.App == nil {
		return BackendRestartSnapshot{}
	}
	return s.MaintenanceTracker(key).RestartState()
}

// Active reports whether maintenance is active for the given key.
func (s MaintenanceStateService) Active(key BackendKey) bool {
	if s.App == nil {
		return false
	}
	return s.MaintenanceTracker(key).Active()
}

// BeginUpgrade starts an upgrade.
func (s MaintenanceStateService) BeginUpgrade(key BackendKey, snapshot BackendUpgradeSnapshot) bool {
	if s.App == nil {
		return false
	}
	return s.MaintenanceTracker(key).BeginUpgrade(snapshot)
}

// BeginRestart starts a restart.
func (s MaintenanceStateService) BeginRestart(key BackendKey, snapshot BackendRestartSnapshot) bool {
	if s.App == nil {
		return false
	}
	return s.MaintenanceTracker(key).BeginRestart(snapshot)
}

// UpdateUpgrade applies a mutation to the upgrade snapshot.
func (s MaintenanceStateService) UpdateUpgrade(key BackendKey, mutate func(*BackendUpgradeSnapshot)) BackendUpgradeSnapshot {
	if s.App == nil {
		return BackendUpgradeSnapshot{}
	}
	return s.MaintenanceTracker(key).UpdateUpgrade(mutate)
}

// UpdateRestart applies a mutation to the restart snapshot.
func (s MaintenanceStateService) UpdateRestart(key BackendKey, mutate func(*BackendRestartSnapshot)) BackendRestartSnapshot {
	if s.App == nil {
		return BackendRestartSnapshot{}
	}
	return s.MaintenanceTracker(key).UpdateRestart(mutate)
}

// FinishUpgrade completes an upgrade.
func (s MaintenanceStateService) FinishUpgrade(key BackendKey, result, message string) BackendUpgradeSnapshot {
	return s.MaintenanceTracker(key).FinishUpgrade(result, message)
}

// FinishRestart completes a restart.
func (s MaintenanceStateService) FinishRestart(key BackendKey, result, message string) BackendRestartSnapshot {
	return s.MaintenanceTracker(key).FinishRestart(result, message)
}

// BlockingPendingCount returns the count of pending requests that block upgrades.
func (s MaintenanceStateService) BlockingPendingCount() int {
	if s.App == nil || s.App.Store() == nil {
		return 0
	}
	count := 0
	for _, req := range s.App.Store().AllPendingRequests() {
		if req == nil || !applifecycle.IsServerResolvedPendingKind(req.Kind) || !applifecycle.IsPendingRequestOpen(req) {
			continue
		}
		count++
	}
	return count
}

// RuntimeBusyReason returns a reason if the runtime is busy, or empty string.
func (s MaintenanceStateService) RuntimeBusyReason(key BackendKey) string {
	if s.App == nil || s.App.Store() == nil {
		return ""
	}
	activeSessions := 0
	for _, sess := range s.App.Store().AllSessions() {
		if sess != nil && appcore.SessionBelongsToFrontend(s.App, sess.Key) && SessionHasActiveWork(sess) {
			activeSessions++
		}
	}
	if activeSessions > 0 {
		return "当前仍有运行中的任务"
	}
	if pendingCount := s.BlockingPendingCount(); pendingCount > 0 {
		return "当前仍有待处理审批或表单"
	}
	return ""
}

// EnsureUpgradeReady checks if upgrade is possible.
func (s MaintenanceStateService) EnsureUpgradeReady(key BackendKey, displayName string) error {
	if s.App == nil {
		return nil
	}
	if s.Active(key) {
		return ErrString(displayName + " 正在维护中，请稍后再试")
	}
	if reason := s.RuntimeBusyReason(key); strings.TrimSpace(reason) != "" {
		return ErrString(reason)
	}
	return nil
}

// AllowsCommand reports whether a command is allowed during maintenance.
func (s MaintenanceStateService) AllowsCommand(raw string, commandName string) bool {
	return AllowsCommand(raw, commandName)
}

// BlocksCommand returns an error if the command is blocked.
func (s MaintenanceStateService) BlocksCommand(key BackendKey, raw string, commandName string, displayName string) error {
	return BlocksCommand(s.MaintenanceTracker(key), raw, commandName, displayName)
}

// Codex convenience wrappers

func (s MaintenanceStateService) CodexMaintenanceTracker() *MaintenanceTracker {
	return s.MaintenanceTracker(BackendKeyCodex)
}
func (s MaintenanceStateService) CodexUpgradeState() BackendUpgradeSnapshot {
	return s.UpgradeState(BackendKeyCodex)
}
func (s MaintenanceStateService) CodexRestartState() BackendRestartSnapshot {
	return s.RestartState(BackendKeyCodex)
}
func (s MaintenanceStateService) CodexMaintenanceActive() bool {
	return s.Active(BackendKeyCodex)
}
func (s MaintenanceStateService) BeginCodexUpgrade(snapshot BackendUpgradeSnapshot) bool {
	return s.BeginUpgrade(BackendKeyCodex, snapshot)
}
func (s MaintenanceStateService) BeginCodexRestart(snapshot BackendRestartSnapshot) bool {
	return s.BeginRestart(BackendKeyCodex, snapshot)
}
func (s MaintenanceStateService) UpdateCodexUpgrade(mutate func(*BackendUpgradeSnapshot)) BackendUpgradeSnapshot {
	return s.UpdateUpgrade(BackendKeyCodex, mutate)
}
func (s MaintenanceStateService) UpdateCodexRestart(mutate func(*BackendRestartSnapshot)) BackendRestartSnapshot {
	return s.UpdateRestart(BackendKeyCodex, mutate)
}
func (s MaintenanceStateService) FinishCodexUpgrade(result, message string) BackendUpgradeSnapshot {
	return s.FinishUpgrade(BackendKeyCodex, result, message)
}
func (s MaintenanceStateService) FinishCodexRestart(result, message string) BackendRestartSnapshot {
	return s.FinishRestart(BackendKeyCodex, result, message)
}
func (s MaintenanceStateService) CodexUpgradeRuntimeBusyReason() string {
	return s.RuntimeBusyReason(BackendKeyCodex)
}
func (s MaintenanceStateService) CodexUpgradeBlockingPendingCount() int {
	return s.BlockingPendingCount()
}
func (s MaintenanceStateService) EnsureCodexUpgradeReady() error {
	return s.EnsureUpgradeReady(BackendKeyCodex, "Codex")
}
func (s MaintenanceStateService) CodexMaintenanceAllowsCommand(raw string) bool {
	return s.AllowsCommand(raw, "/codex")
}
func (s MaintenanceStateService) CodexMaintenanceBlocksCommand(raw string) error {
	return s.BlocksCommand(BackendKeyCodex, raw, "/codex", "Codex")
}

// Claude convenience wrappers

func (s MaintenanceStateService) ClaudeMaintenanceTracker() *MaintenanceTracker {
	return s.MaintenanceTracker(BackendKeyClaude)
}
func (s MaintenanceStateService) ClaudeUpgradeState() BackendUpgradeSnapshot {
	return s.UpgradeState(BackendKeyClaude)
}
func (s MaintenanceStateService) ClaudeRestartState() BackendRestartSnapshot {
	return s.RestartState(BackendKeyClaude)
}
func (s MaintenanceStateService) ClaudeMaintenanceActive() bool {
	return s.Active(BackendKeyClaude)
}
func (s MaintenanceStateService) BeginClaudeUpgrade(snapshot BackendUpgradeSnapshot) bool {
	return s.BeginUpgrade(BackendKeyClaude, snapshot)
}
func (s MaintenanceStateService) BeginClaudeRestart(snapshot BackendRestartSnapshot) bool {
	return s.BeginRestart(BackendKeyClaude, snapshot)
}
func (s MaintenanceStateService) UpdateClaudeUpgrade(mutate func(*BackendUpgradeSnapshot)) BackendUpgradeSnapshot {
	return s.UpdateUpgrade(BackendKeyClaude, mutate)
}
func (s MaintenanceStateService) UpdateClaudeRestart(mutate func(*BackendRestartSnapshot)) BackendRestartSnapshot {
	return s.UpdateRestart(BackendKeyClaude, mutate)
}
func (s MaintenanceStateService) FinishClaudeUpgrade(result, message string) BackendUpgradeSnapshot {
	return s.FinishUpgrade(BackendKeyClaude, result, message)
}
func (s MaintenanceStateService) FinishClaudeRestart(result, message string) BackendRestartSnapshot {
	return s.FinishRestart(BackendKeyClaude, result, message)
}
func (s MaintenanceStateService) ClaudeUpgradeRuntimeBusyReason() string {
	return s.RuntimeBusyReason(BackendKeyClaude)
}
func (s MaintenanceStateService) ClaudeUpgradeBlockingPendingCount() int {
	return s.BlockingPendingCount()
}
func (s MaintenanceStateService) EnsureClaudeUpgradeReady() error {
	return s.EnsureUpgradeReady(BackendKeyClaude, "Claude")
}
func (s MaintenanceStateService) ClaudeMaintenanceAllowsCommand(raw string) bool {
	return s.AllowsCommand(raw, "/claude")
}
func (s MaintenanceStateService) ClaudeMaintenanceBlocksCommand(raw string) error {
	return s.BlocksCommand(BackendKeyClaude, raw, "/claude", "Claude")
}
