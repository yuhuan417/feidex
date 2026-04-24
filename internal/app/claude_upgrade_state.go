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

func (s maintenanceStateService) claudeMaintenanceTracker() *claudeMaintenanceTracker {
	if s.app == nil {
		return nil
	}
	if s.app.claudeMaintenance == nil {
		s.app.claudeMaintenance = newClaudeMaintenanceTracker()
	}
	return s.app.claudeMaintenance
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

func (s maintenanceStateService) claudeUpgradeState() claudeUpgradeSnapshot {
	if s.app == nil {
		return claudeUpgradeSnapshot{}
	}
	tracker := s.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.upgrade
}

func (s maintenanceStateService) claudeRestartState() claudeRestartSnapshot {
	if s.app == nil {
		return claudeRestartSnapshot{}
	}
	tracker := s.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.restart
}

func (s maintenanceStateService) claudeMaintenanceActive() bool {
	if s.app == nil {
		return false
	}
	tracker := s.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.upgrade.Running || tracker.restart.Running
}

func (s maintenanceStateService) beginClaudeUpgrade(snapshot claudeUpgradeSnapshot) bool {
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

	tracker := s.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.upgrade.Running || tracker.restart.Running {
		return false
	}
	tracker.upgrade = snapshot
	return true
}

func (s maintenanceStateService) beginClaudeRestart(snapshot claudeRestartSnapshot) bool {
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

	tracker := s.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.upgrade.Running || tracker.restart.Running {
		return false
	}
	tracker.restart = snapshot
	return true
}

func (s maintenanceStateService) updateClaudeUpgrade(mutate func(*claudeUpgradeSnapshot)) claudeUpgradeSnapshot {
	if s.app == nil {
		return claudeUpgradeSnapshot{}
	}
	tracker := s.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	mutate(&tracker.upgrade)
	tracker.upgrade.UpdatedAt = time.Now()
	return tracker.upgrade
}

func (s maintenanceStateService) updateClaudeRestart(mutate func(*claudeRestartSnapshot)) claudeRestartSnapshot {
	if s.app == nil {
		return claudeRestartSnapshot{}
	}
	tracker := s.claudeMaintenanceTracker()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	mutate(&tracker.restart)
	tracker.restart.UpdatedAt = time.Now()
	return tracker.restart
}

func (s maintenanceStateService) finishClaudeUpgrade(result, message string) claudeUpgradeSnapshot {
	return s.updateClaudeUpgrade(func(snapshot *claudeUpgradeSnapshot) {
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

func (s maintenanceStateService) finishClaudeRestart(result, message string) claudeRestartSnapshot {
	return s.updateClaudeRestart(func(snapshot *claudeRestartSnapshot) {
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

func (s maintenanceStateService) claudeUpgradeRuntimeBusyReason() string {
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
	if pendingCount := s.claudeUpgradeBlockingPendingCount(); pendingCount > 0 {
		return "当前仍有待处理审批或表单"
	}
	return ""
}

func (s maintenanceStateService) claudeUpgradeBlockingPendingCount() int {
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

func (s maintenanceStateService) ensureClaudeUpgradeReady() error {
	if s.app == nil {
		return nil
	}
	if s.claudeMaintenanceActive() {
		return errString("Claude 正在维护中，请稍后再试")
	}
	if reason := s.claudeUpgradeRuntimeBusyReason(); strings.TrimSpace(reason) != "" {
		return errString(reason)
	}
	return nil
}

func (s maintenanceStateService) claudeMaintenanceAllowsCommand(raw string) bool {
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

func (s maintenanceStateService) claudeMaintenanceBlocksCommand(raw string) error {
	if s.app == nil || !s.claudeMaintenanceActive() {
		return nil
	}
	if s.claudeMaintenanceAllowsCommand(raw) {
		return nil
	}
	return errString("Claude 正在维护中，当前只允许 `/claude`、`/status`、`/help`")
}
