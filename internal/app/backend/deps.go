// Package backend provides backend selection, configuration, transition state,
// maintenance, and recovery infrastructure extracted from the app package.
package backend

import (
	"sync"

	"feidex/internal/app/appcore"
)

// Dependencies is the capability set consumed by backend services. The alias
// below exists only for source compatibility with older composition literals.
type Dependencies interface {
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

// App is a compatibility name for Dependencies; new code should use
// Dependencies explicitly.
type App = Dependencies
