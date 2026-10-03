package runtime

import (
	"feidex/internal/application/backendmaintenance"
	"strings"
	"sync"
	"time"
)

// MaintenanceTracker tracks upgrade/restart state for a single backend.
type MaintenanceTracker struct {
	mu      sync.Mutex
	Upgrade BackendUpgradeSnapshot
	Restart BackendRestartSnapshot
}

// NewMaintenanceTracker creates a new tracker.
func NewMaintenanceTracker() *MaintenanceTracker {
	return &MaintenanceTracker{}
}

// TrackerMap is a map of backend keys to maintenance trackers.
type TrackerMap map[BackendKey]*MaintenanceTracker

// Get returns the tracker fixed by frontend composition.
func (m TrackerMap) Get(key BackendKey) *MaintenanceTracker {
	return m[key]
}

func (m TrackerMap) Tracker(key BackendKey) backendmaintenance.State { return m[key] }

// UpgradeState returns a snapshot of the upgrade state.
func (t *MaintenanceTracker) UpgradeState() BackendUpgradeSnapshot {
	if t == nil {
		return BackendUpgradeSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Upgrade
}

// RestartState returns a snapshot of the restart state.
func (t *MaintenanceTracker) RestartState() BackendRestartSnapshot {
	if t == nil {
		return BackendRestartSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Restart
}

// Active reports whether an upgrade or restart is running.
func (t *MaintenanceTracker) Active() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Upgrade.Running || t.Restart.Running
}

// BeginUpgrade starts an upgrade. Returns false if already in maintenance.
func (t *MaintenanceTracker) BeginUpgrade(snapshot BackendUpgradeSnapshot) bool {
	if t == nil {
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

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Upgrade.Running || t.Restart.Running {
		return false
	}
	t.Upgrade = snapshot
	return true
}

// BeginRestart starts a restart. Returns false if already in maintenance.
func (t *MaintenanceTracker) BeginRestart(snapshot BackendRestartSnapshot) bool {
	if t == nil {
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

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Upgrade.Running || t.Restart.Running {
		return false
	}
	t.Restart = snapshot
	return true
}

// UpdateUpgrade applies a mutation to the upgrade snapshot.
func (t *MaintenanceTracker) UpdateUpgrade(mutate func(*BackendUpgradeSnapshot)) BackendUpgradeSnapshot {
	if t == nil {
		return BackendUpgradeSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	mutate(&t.Upgrade)
	t.Upgrade.UpdatedAt = time.Now()
	return t.Upgrade
}

// UpdateRestart applies a mutation to the restart snapshot.
func (t *MaintenanceTracker) UpdateRestart(mutate func(*BackendRestartSnapshot)) BackendRestartSnapshot {
	if t == nil {
		return BackendRestartSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	mutate(&t.Restart)
	t.Restart.UpdatedAt = time.Now()
	return t.Restart
}

// FinishUpgrade completes an upgrade with the given result and message.
func (t *MaintenanceTracker) FinishUpgrade(result, message string) BackendUpgradeSnapshot {
	return t.UpdateUpgrade(func(snapshot *BackendUpgradeSnapshot) {
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

// FinishRestart completes a restart with the given result and message.
func (t *MaintenanceTracker) FinishRestart(result, message string) BackendRestartSnapshot {
	return t.UpdateRestart(func(snapshot *BackendRestartSnapshot) {
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
