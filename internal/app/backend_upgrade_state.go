package app

import (
	"strings"
	"sync"
	"time"
)

type backendKey string

const (
	backendKeyCodex  backendKey = "codex"
	backendKeyClaude backendKey = "claude"
)

type maintenanceStateService struct {
	app *App
}

func newMaintenanceStateService(app *App) maintenanceStateService {
	return maintenanceStateService{app: app}
}

type backendMaintenanceTracker struct {
	mu      sync.Mutex
	upgrade backendUpgradeSnapshot
	restart backendRestartSnapshot
}

func newBackendMaintenanceTracker() *backendMaintenanceTracker {
	return &backendMaintenanceTracker{}
}

type backendUpgradeSnapshot struct {
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

type backendRestartSnapshot struct {
	Running        bool
	Phase          string
	Result         string
	Message        string
	CurrentVersion string
	StartedAt      time.Time
	UpdatedAt      time.Time
}

func (s maintenanceStateService) maintenanceTracker(key backendKey) *backendMaintenanceTracker {
	if s.app == nil {
		return nil
	}
	if s.app.maintenanceTrackers == nil {
		s.app.maintenanceTrackers = make(map[backendKey]*backendMaintenanceTracker)
	}
	tracker := s.app.maintenanceTrackers[key]
	if tracker == nil {
		tracker = newBackendMaintenanceTracker()
		s.app.maintenanceTrackers[key] = tracker
	}
	return tracker
}

func (s maintenanceStateService) upgradeState(key backendKey) backendUpgradeSnapshot {
	if s.app == nil {
		return backendUpgradeSnapshot{}
	}
	tracker := s.maintenanceTracker(key)
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.upgrade
}

func (s maintenanceStateService) restartState(key backendKey) backendRestartSnapshot {
	if s.app == nil {
		return backendRestartSnapshot{}
	}
	tracker := s.maintenanceTracker(key)
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.restart
}

func (s maintenanceStateService) maintenanceActive(key backendKey) bool {
	if s.app == nil {
		return false
	}
	tracker := s.maintenanceTracker(key)
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.upgrade.Running || tracker.restart.Running
}

func (s maintenanceStateService) beginUpgrade(key backendKey, snapshot backendUpgradeSnapshot) bool {
	if s.app == nil {
		return false
	}
	now := time.Now()
	snapshot.Running = true
	if snapshot.Phase == "" {
		snapshot.Phase = "preflight"
	}
	if snapshot.StartedAt.IsZero() {
		snapshot.StartedAt = now
	}
	snapshot.UpdatedAt = now

	tracker := s.maintenanceTracker(key)
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.upgrade.Running || tracker.restart.Running {
		return false
	}
	tracker.upgrade = snapshot
	return true
}

func (s maintenanceStateService) beginRestart(key backendKey, snapshot backendRestartSnapshot) bool {
	if s.app == nil {
		return false
	}
	now := time.Now()
	snapshot.Running = true
	if snapshot.Phase == "" {
		snapshot.Phase = "preflight"
	}
	if snapshot.StartedAt.IsZero() {
		snapshot.StartedAt = now
	}
	snapshot.UpdatedAt = now

	tracker := s.maintenanceTracker(key)
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.upgrade.Running || tracker.restart.Running {
		return false
	}
	tracker.restart = snapshot
	return true
}

func (s maintenanceStateService) updateUpgrade(key backendKey, mutate func(*backendUpgradeSnapshot)) backendUpgradeSnapshot {
	if s.app == nil {
		return backendUpgradeSnapshot{}
	}
	tracker := s.maintenanceTracker(key)
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	mutate(&tracker.upgrade)
	tracker.upgrade.UpdatedAt = time.Now()
	return tracker.upgrade
}

func (s maintenanceStateService) updateRestart(key backendKey, mutate func(*backendRestartSnapshot)) backendRestartSnapshot {
	if s.app == nil {
		return backendRestartSnapshot{}
	}
	tracker := s.maintenanceTracker(key)
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	mutate(&tracker.restart)
	tracker.restart.UpdatedAt = time.Now()
	return tracker.restart
}

func (s maintenanceStateService) finishUpgrade(key backendKey, result, message string) backendUpgradeSnapshot {
	return s.updateUpgrade(key, func(snapshot *backendUpgradeSnapshot) {
		snapshot.Running = false
		if strings.TrimSpace(result) != "" {
			snapshot.Result = strings.TrimSpace(result)
		}
		if strings.TrimSpace(message) != "" {
			snapshot.Message = strings.TrimSpace(message)
		}
		switch snapshot.Result {
		case "success":
			snapshot.Phase = "completed"
			if strings.TrimSpace(snapshot.TargetVersion) != "" {
				snapshot.CurrentVersion = strings.TrimSpace(snapshot.TargetVersion)
			}
		case "rolled_back":
			snapshot.Phase = "failed"
			if strings.TrimSpace(snapshot.PreviousVersion) != "" {
				snapshot.CurrentVersion = strings.TrimSpace(snapshot.PreviousVersion)
			}
		case "rollback_failed":
			snapshot.Phase = "failed"
		default:
			if snapshot.Phase == "" {
				snapshot.Phase = "failed"
			}
		}
	})
}

func (s maintenanceStateService) finishRestart(key backendKey, result, message string) backendRestartSnapshot {
	return s.updateRestart(key, func(snapshot *backendRestartSnapshot) {
		snapshot.Running = false
		if strings.TrimSpace(result) != "" {
			snapshot.Result = strings.TrimSpace(result)
		}
		if strings.TrimSpace(message) != "" {
			snapshot.Message = strings.TrimSpace(message)
		}
		switch snapshot.Result {
		case "success":
			snapshot.Phase = "completed"
		default:
			snapshot.Phase = "failed"
		}
	})
}

func (s maintenanceStateService) upgradeBlockingPendingCount() int {
	if s.app == nil || s.app.store == nil {
		return 0
	}
	count := 0
	for _, req := range appState(s.app).pendingRequests() {
		if req == nil || !isServerResolvedPendingKind(req.Kind) || !isPendingRequestOpen(req) {
			continue
		}
		count++
	}
	return count
}

func (s maintenanceStateService) upgradeRuntimeBusyReason(key backendKey) string {
	if s.app == nil || s.app.store == nil {
		return ""
	}
	activeSessions := 0
	for _, sess := range appState(s.app).sessions() {
		if sess != nil && sessionBelongsToFrontend(s.app, sess.Key) && sessionHasActiveWork(sess) {
			activeSessions++
		}
	}
	if activeSessions > 0 {
		return "当前仍有运行中的任务"
	}
	if pendingCount := s.upgradeBlockingPendingCount(); pendingCount > 0 {
		return "当前仍有待处理审批或表单"
	}
	return ""
}

func (s maintenanceStateService) ensureUpgradeReady(key backendKey, displayName string) error {
	if s.app == nil {
		return nil
	}
	if s.maintenanceActive(key) {
		return errString(displayName + " 正在维护中，请稍后再试")
	}
	if reason := s.upgradeRuntimeBusyReason(key); strings.TrimSpace(reason) != "" {
		return errString(reason)
	}
	return nil
}

func (s maintenanceStateService) maintenanceAllowsCommand(raw string, commandName string) bool {
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

func (s maintenanceStateService) maintenanceBlocksCommand(key backendKey, raw string, commandName string, displayName string) error {
	if s.app == nil || !s.maintenanceActive(key) {
		return nil
	}
	if s.maintenanceAllowsCommand(raw, commandName) {
		return nil
	}
	return errString(displayName + " 正在维护中，当前只允许 `" + commandName + "`、`/status`、`/help`")
}

// Codex convenience wrappers

func (s maintenanceStateService) codexMaintenanceTracker() *backendMaintenanceTracker {
	return s.maintenanceTracker(backendKeyCodex)
}

func (s maintenanceStateService) codexUpgradeState() backendUpgradeSnapshot {
	return s.upgradeState(backendKeyCodex)
}

func (s maintenanceStateService) codexRestartState() backendRestartSnapshot {
	return s.restartState(backendKeyCodex)
}

func (s maintenanceStateService) codexMaintenanceActive() bool {
	return s.maintenanceActive(backendKeyCodex)
}

func (s maintenanceStateService) beginCodexUpgrade(snapshot backendUpgradeSnapshot) bool {
	return s.beginUpgrade(backendKeyCodex, snapshot)
}

func (s maintenanceStateService) beginCodexRestart(snapshot backendRestartSnapshot) bool {
	return s.beginRestart(backendKeyCodex, snapshot)
}

func (s maintenanceStateService) updateCodexUpgrade(mutate func(*backendUpgradeSnapshot)) backendUpgradeSnapshot {
	return s.updateUpgrade(backendKeyCodex, mutate)
}

func (s maintenanceStateService) updateCodexRestart(mutate func(*backendRestartSnapshot)) backendRestartSnapshot {
	return s.updateRestart(backendKeyCodex, mutate)
}

func (s maintenanceStateService) finishCodexUpgrade(result, message string) backendUpgradeSnapshot {
	return s.finishUpgrade(backendKeyCodex, result, message)
}

func (s maintenanceStateService) finishCodexRestart(result, message string) backendRestartSnapshot {
	return s.finishRestart(backendKeyCodex, result, message)
}

func (s maintenanceStateService) codexUpgradeRuntimeBusyReason() string {
	return s.upgradeRuntimeBusyReason(backendKeyCodex)
}

func (s maintenanceStateService) codexUpgradeBlockingPendingCount() int {
	return s.upgradeBlockingPendingCount()
}

func (s maintenanceStateService) ensureCodexUpgradeReady() error {
	return s.ensureUpgradeReady(backendKeyCodex, "Codex")
}

func (s maintenanceStateService) codexMaintenanceAllowsCommand(raw string) bool {
	return s.maintenanceAllowsCommand(raw, "/codex")
}

func (s maintenanceStateService) codexMaintenanceBlocksCommand(raw string) error {
	return s.maintenanceBlocksCommand(backendKeyCodex, raw, "/codex", "Codex")
}

// Claude convenience wrappers

func (s maintenanceStateService) claudeMaintenanceTracker() *backendMaintenanceTracker {
	return s.maintenanceTracker(backendKeyClaude)
}

func (s maintenanceStateService) claudeUpgradeState() backendUpgradeSnapshot {
	return s.upgradeState(backendKeyClaude)
}

func (s maintenanceStateService) claudeRestartState() backendRestartSnapshot {
	return s.restartState(backendKeyClaude)
}

func (s maintenanceStateService) claudeMaintenanceActive() bool {
	return s.maintenanceActive(backendKeyClaude)
}

func (s maintenanceStateService) beginClaudeUpgrade(snapshot backendUpgradeSnapshot) bool {
	return s.beginUpgrade(backendKeyClaude, snapshot)
}

func (s maintenanceStateService) beginClaudeRestart(snapshot backendRestartSnapshot) bool {
	return s.beginRestart(backendKeyClaude, snapshot)
}

func (s maintenanceStateService) updateClaudeUpgrade(mutate func(*backendUpgradeSnapshot)) backendUpgradeSnapshot {
	return s.updateUpgrade(backendKeyClaude, mutate)
}

func (s maintenanceStateService) updateClaudeRestart(mutate func(*backendRestartSnapshot)) backendRestartSnapshot {
	return s.updateRestart(backendKeyClaude, mutate)
}

func (s maintenanceStateService) finishClaudeUpgrade(result, message string) backendUpgradeSnapshot {
	return s.finishUpgrade(backendKeyClaude, result, message)
}

func (s maintenanceStateService) finishClaudeRestart(result, message string) backendRestartSnapshot {
	return s.finishRestart(backendKeyClaude, result, message)
}

func (s maintenanceStateService) claudeUpgradeRuntimeBusyReason() string {
	return s.upgradeRuntimeBusyReason(backendKeyClaude)
}

func (s maintenanceStateService) claudeUpgradeBlockingPendingCount() int {
	return s.upgradeBlockingPendingCount()
}

func (s maintenanceStateService) ensureClaudeUpgradeReady() error {
	return s.ensureUpgradeReady(backendKeyClaude, "Claude")
}

func (s maintenanceStateService) claudeMaintenanceAllowsCommand(raw string) bool {
	return s.maintenanceAllowsCommand(raw, "/claude")
}

func (s maintenanceStateService) claudeMaintenanceBlocksCommand(raw string) error {
	return s.maintenanceBlocksCommand(backendKeyClaude, raw, "/claude", "Claude")
}

type errString string

func (e errString) Error() string { return string(e) }
