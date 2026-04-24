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

func (s maintenanceStateService) codexMaintenanceTracker() *codexMaintenanceTracker {
	if s.app == nil {
		return nil
	}
	if s.app.codexMaintenance == nil {
		s.app.codexMaintenance = newCodexMaintenanceTracker()
	}
	return s.app.codexMaintenance
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

func (s maintenanceStateService) codexUpgradeState() codexUpgradeSnapshot {
	if s.app == nil {
		return codexUpgradeSnapshot{}
	}
	tracker := s.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.upgrade
}

func (s maintenanceStateService) codexRestartState() codexRestartSnapshot {
	if s.app == nil {
		return codexRestartSnapshot{}
	}
	tracker := s.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.restart
}

func (s maintenanceStateService) codexMaintenanceActive() bool {
	if s.app == nil {
		return false
	}
	tracker := s.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.upgrade.Running || tracker.restart.Running
}

func (s maintenanceStateService) beginCodexUpgrade(snapshot codexUpgradeSnapshot) bool {
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

	tracker := s.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.upgrade.Running || tracker.restart.Running {
		return false
	}
	tracker.upgrade = snapshot
	return true
}

func (s maintenanceStateService) beginCodexRestart(snapshot codexRestartSnapshot) bool {
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

	tracker := s.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.upgrade.Running || tracker.restart.Running {
		return false
	}
	tracker.restart = snapshot
	return true
}

func (s maintenanceStateService) updateCodexUpgrade(mutate func(*codexUpgradeSnapshot)) codexUpgradeSnapshot {
	if s.app == nil {
		return codexUpgradeSnapshot{}
	}
	tracker := s.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	mutate(&tracker.upgrade)
	tracker.upgrade.UpdatedAt = time.Now()
	return tracker.upgrade
}

func (s maintenanceStateService) updateCodexRestart(mutate func(*codexRestartSnapshot)) codexRestartSnapshot {
	if s.app == nil {
		return codexRestartSnapshot{}
	}
	tracker := s.codexMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	mutate(&tracker.restart)
	tracker.restart.UpdatedAt = time.Now()
	return tracker.restart
}

func (s maintenanceStateService) finishCodexUpgrade(result, message string) codexUpgradeSnapshot {
	return s.updateCodexUpgrade(func(snapshot *codexUpgradeSnapshot) {
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

func (s maintenanceStateService) finishCodexRestart(result, message string) codexRestartSnapshot {
	return s.updateCodexRestart(func(snapshot *codexRestartSnapshot) {
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

func (s maintenanceStateService) codexUpgradeBlockingPendingCount() int {
	if s.app == nil || s.app.store == nil {
		return 0
	}
	count := 0
	for _, req := range s.app.appState().pendingRequests() {
		if req == nil || !isServerResolvedPendingKind(req.Kind) || !isPendingRequestOpen(req) {
			continue
		}
		count++
	}
	return count
}

func (s maintenanceStateService) codexUpgradeRuntimeBusyReason() string {
	if s.app == nil || s.app.store == nil {
		return ""
	}
	activeSessions := 0
	for _, sess := range s.app.appState().sessions() {
		if sess != nil && sessionBelongsToFrontend(s.app, sess.Key) && sessionHasActiveWork(sess) {
			activeSessions++
		}
	}
	if activeSessions > 0 {
		return "当前仍有运行中的任务"
	}
	if pendingCount := s.codexUpgradeBlockingPendingCount(); pendingCount > 0 {
		return "当前仍有待处理审批或表单"
	}
	return ""
}

func (s maintenanceStateService) ensureCodexUpgradeReady() error {
	if s.app == nil {
		return nil
	}
	if s.codexMaintenanceActive() {
		return errString("Codex 正在维护中，请稍后再试")
	}
	if reason := s.codexUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		return errString(reason)
	}
	return nil
}

func (s maintenanceStateService) codexMaintenanceAllowsCommand(raw string) bool {
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

func (s maintenanceStateService) codexMaintenanceBlocksCommand(raw string) error {
	if s.app == nil || !s.codexMaintenanceActive() {
		return nil
	}
	if s.codexMaintenanceAllowsCommand(raw) {
		return nil
	}
	return errString("Codex 正在维护中，当前只允许 `/codex`、`/status`、`/help`")
}

type errString string

func (e errString) Error() string { return string(e) }
