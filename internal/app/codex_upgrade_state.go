package app

import (
	"strings"
	"sync"
	"time"
)

type codexMaintenanceTracker struct {
	mu      sync.Mutex
	upgrade codexUpgradeSnapshot
	restart codexRestartSnapshot
}

func newCodexMaintenanceTracker() *codexMaintenanceTracker {
	return &codexMaintenanceTracker{}
}

func (a *App) codexMaintenanceTracker() *codexMaintenanceTracker {
	if a == nil {
		return nil
	}
	if a.codexMaintenance == nil {
		a.codexMaintenance = newCodexMaintenanceTracker()
	}
	return a.codexMaintenance
}

type codexUpgradeSnapshot struct {
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

type codexRestartSnapshot struct {
	Running        bool
	Phase          string
	Result         string
	Message        string
	CurrentVersion string
	StartedAt      time.Time
	UpdatedAt      time.Time
}

func (a *App) codexUpgradeState() codexUpgradeSnapshot {
	if a == nil {
		return codexUpgradeSnapshot{}
	}
	tracker := a.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.upgrade
}

func (a *App) codexRestartState() codexRestartSnapshot {
	if a == nil {
		return codexRestartSnapshot{}
	}
	tracker := a.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.restart
}

func (a *App) codexMaintenanceActive() bool {
	if a == nil {
		return false
	}
	tracker := a.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.upgrade.Running || tracker.restart.Running
}

func (a *App) beginCodexUpgrade(snapshot codexUpgradeSnapshot) bool {
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

	tracker := a.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.upgrade.Running || tracker.restart.Running {
		return false
	}
	tracker.upgrade = snapshot
	return true
}

func (a *App) beginCodexRestart(snapshot codexRestartSnapshot) bool {
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

	tracker := a.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.upgrade.Running || tracker.restart.Running {
		return false
	}
	tracker.restart = snapshot
	return true
}

func (a *App) updateCodexUpgrade(mutate func(*codexUpgradeSnapshot)) codexUpgradeSnapshot {
	if a == nil {
		return codexUpgradeSnapshot{}
	}
	tracker := a.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	mutate(&tracker.upgrade)
	tracker.upgrade.UpdatedAt = time.Now()
	return tracker.upgrade
}

func (a *App) updateCodexRestart(mutate func(*codexRestartSnapshot)) codexRestartSnapshot {
	if a == nil {
		return codexRestartSnapshot{}
	}
	tracker := a.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	mutate(&tracker.restart)
	tracker.restart.UpdatedAt = time.Now()
	return tracker.restart
}

func (a *App) finishCodexUpgrade(result, message string) codexUpgradeSnapshot {
	return a.updateCodexUpgrade(func(snapshot *codexUpgradeSnapshot) {
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

func (a *App) finishCodexRestart(result, message string) codexRestartSnapshot {
	return a.updateCodexRestart(func(snapshot *codexRestartSnapshot) {
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

func (a *App) codexUpgradeBlockingPendingCount() int {
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

func (a *App) codexUpgradeRuntimeBusyReason() string {
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
	if pendingCount := a.codexUpgradeBlockingPendingCount(); pendingCount > 0 {
		return "当前仍有待处理审批或表单"
	}
	return ""
}

func (a *App) ensureCodexUpgradeReady() error {
	if a == nil {
		return nil
	}
	if a.codexMaintenanceActive() {
		return errString("Codex 正在维护中，请稍后再试")
	}
	if reason := a.codexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		return errString(reason)
	}
	return nil
}

func (a *App) codexMaintenanceAllowsCommand(raw string) bool {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "/help":
		return true
	case raw == "/status":
		return true
	case raw == "/codex":
		return true
	case strings.HasPrefix(raw, "/codex "):
		return true
	default:
		return false
	}
}

func (a *App) codexMaintenanceBlocksCommand(raw string) error {
	if a == nil || !a.codexMaintenanceActive() {
		return nil
	}
	if a.codexMaintenanceAllowsCommand(raw) {
		return nil
	}
	return errString("Codex 正在维护中，当前只允许 `/codex`、`/status`、`/help`")
}

type errString string

func (e errString) Error() string { return string(e) }
