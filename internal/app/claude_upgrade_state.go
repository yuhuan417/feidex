package app

import (
	"strings"
	"sync"
	"time"
)

type claudeMaintenanceTracker struct {
	mu      sync.Mutex
	upgrade claudeUpgradeSnapshot
	restart claudeRestartSnapshot
}

func newClaudeMaintenanceTracker() *claudeMaintenanceTracker {
	return &claudeMaintenanceTracker{}
}

func (a *App) claudeMaintenanceTracker() *claudeMaintenanceTracker {
	if a == nil {
		return nil
	}
	if a.claudeMaintenance == nil {
		a.claudeMaintenance = newClaudeMaintenanceTracker()
	}
	return a.claudeMaintenance
}

type claudeUpgradeSnapshot struct {
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

type claudeRestartSnapshot struct {
	Running        bool
	Phase          string
	Result         string
	Message        string
	CurrentVersion string
	StartedAt      time.Time
	UpdatedAt      time.Time
}

func (a *App) claudeUpgradeState() claudeUpgradeSnapshot {
	if a == nil {
		return claudeUpgradeSnapshot{}
	}
	tracker := a.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.upgrade
}

func (a *App) claudeRestartState() claudeRestartSnapshot {
	if a == nil {
		return claudeRestartSnapshot{}
	}
	tracker := a.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.restart
}

func (a *App) claudeMaintenanceActive() bool {
	if a == nil {
		return false
	}
	tracker := a.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.upgrade.Running || tracker.restart.Running
}

func (a *App) beginClaudeUpgrade(snapshot claudeUpgradeSnapshot) bool {
	if a == nil {
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

	tracker := a.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.upgrade.Running || tracker.restart.Running {
		return false
	}
	tracker.upgrade = snapshot
	return true
}

func (a *App) beginClaudeRestart(snapshot claudeRestartSnapshot) bool {
	if a == nil {
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

	tracker := a.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.upgrade.Running || tracker.restart.Running {
		return false
	}
	tracker.restart = snapshot
	return true
}

func (a *App) updateClaudeUpgrade(mutate func(*claudeUpgradeSnapshot)) claudeUpgradeSnapshot {
	if a == nil {
		return claudeUpgradeSnapshot{}
	}
	tracker := a.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	mutate(&tracker.upgrade)
	tracker.upgrade.UpdatedAt = time.Now()
	return tracker.upgrade
}

func (a *App) updateClaudeRestart(mutate func(*claudeRestartSnapshot)) claudeRestartSnapshot {
	if a == nil {
		return claudeRestartSnapshot{}
	}
	tracker := a.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	mutate(&tracker.restart)
	tracker.restart.UpdatedAt = time.Now()
	return tracker.restart
}

func (a *App) finishClaudeUpgrade(result, message string) claudeUpgradeSnapshot {
	return a.updateClaudeUpgrade(func(snapshot *claudeUpgradeSnapshot) {
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

func (a *App) finishClaudeRestart(result, message string) claudeRestartSnapshot {
	return a.updateClaudeRestart(func(snapshot *claudeRestartSnapshot) {
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

func (a *App) claudeUpgradeRuntimeBusyReason() string {
	if a == nil || a.store == nil {
		return ""
	}
	activeSessions := 0
	for _, sess := range a.appState().sessions() {
		if sess != nil && a.sessionBelongsToFrontend(sess.Key) && sessionHasActiveWork(sess) {
			activeSessions++
		}
	}
	if activeSessions > 0 {
		return "当前仍有运行中的任务"
	}
	if pendingCount := a.claudeUpgradeBlockingPendingCount(); pendingCount > 0 {
		return "当前仍有待处理审批或表单"
	}
	return ""
}

func (a *App) claudeUpgradeBlockingPendingCount() int {
	if a == nil || a.store == nil {
		return 0
	}
	count := 0
	for _, req := range a.appState().pendingRequests() {
		if req == nil || !isServerResolvedPendingKind(req.Kind) || !isPendingRequestOpen(req) {
			continue
		}
		count++
	}
	return count
}

func (a *App) ensureClaudeUpgradeReady() error {
	if a == nil {
		return nil
	}
	if a.claudeMaintenanceActive() {
		return errString("Claude 正在维护中，请稍后再试")
	}
	if reason := a.claudeUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		return errString(reason)
	}
	return nil
}

func (a *App) claudeMaintenanceAllowsCommand(raw string) bool {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "/help":
		return true
	case raw == "/status":
		return true
	case raw == "/claude":
		return true
	case strings.HasPrefix(raw, "/claude "):
		return true
	default:
		return false
	}
}

func (a *App) claudeMaintenanceBlocksCommand(raw string) error {
	if a == nil || !a.claudeMaintenanceActive() {
		return nil
	}
	if a.claudeMaintenanceAllowsCommand(raw) {
		return nil
	}
	return errString("Claude 正在维护中，当前只允许 `/claude`、`/status`、`/help`")
}
