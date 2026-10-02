// Package backend provides backend selection, configuration, transition state,
// maintenance, and recovery infrastructure extracted from the app package.
package backend

import (
	feishutransport "feidex/internal/adapter/feishu/transport"
	"sync"

	"feidex/internal/app/appcore"
)

// Dependencies is the capability set consumed by backend services.
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
	Feishu() feishutransport.Client
}
