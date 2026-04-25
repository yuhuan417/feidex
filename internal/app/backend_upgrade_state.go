package app

import (
	appbackend "feidex/internal/app/backend"
)

type backendKey = appbackend.BackendKey

const (
	backendKeyCodex  = appbackend.BackendKeyCodex
	backendKeyClaude = appbackend.BackendKeyClaude
)

type backendUpgradeSnapshot = appbackend.BackendUpgradeSnapshot
type backendRestartSnapshot = appbackend.BackendRestartSnapshot
type backendMaintenanceTracker = appbackend.MaintenanceTracker

var newBackendMaintenanceTracker = appbackend.NewMaintenanceTracker

type maintenanceStateService = appbackend.MaintenanceStateService

var newMaintenanceStateService = appbackend.NewMaintenanceStateService

type errString = appbackend.ErrString

var now = appbackend.Now
