// Package backend provides backend selection, configuration, transition state,
// maintenance, and recovery infrastructure extracted from the app package.
package backend

import (
	"sync"

	"feidex/internal/app/appcore"
)

// App is the interface that the backend package uses to access *App fields.
// *App satisfies this via its accessor methods.
type App interface {
	appcore.AppExtended

	// Backend switch state
	BackendStateMu() *sync.Mutex
	BackendSwitchMu() *sync.Mutex
	BackendSwitching() bool
	SetBackendSwitching(bool)
	BackendSwitchTarget() string
	SetBackendSwitchTarget(string)

	// Maintenance trackers
	MaintenanceTrackers() TrackerMap

	// Feishu client access
	Feishu() appcore.FeishuClient
}
