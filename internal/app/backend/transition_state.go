package backend

import (
	"strings"

	"feidex/internal/app/appcore"
)

// RuntimeStateService manages backend switch transition state.
type RuntimeStateService struct {
	app App
}

// NewRuntimeStateService creates a new RuntimeStateService.
func NewRuntimeStateService(app App) RuntimeStateService {
	return RuntimeStateService{app: app}
}

// BeginBackendSwitchState marks the start of a backend switch.
func (s RuntimeStateService) BeginBackendSwitchState(target string) {
	if s.app == nil {
		return
	}
	s.app.BackendStateMu().Lock()
	defer s.app.BackendStateMu().Unlock()
	s.app.SetBackendSwitching(true)
	s.app.SetBackendSwitchTarget(appcore.NormalizeRuntimeBackend(target))
}

// FinishBackendSwitchState marks the end of a backend switch.
func (s RuntimeStateService) FinishBackendSwitchState() {
	if s.app == nil {
		return
	}
	s.app.BackendStateMu().Lock()
	defer s.app.BackendStateMu().Unlock()
	s.app.SetBackendSwitching(false)
	s.app.SetBackendSwitchTarget("")
}

// BackendSwitchState returns the current switch state.
func (s RuntimeStateService) BackendSwitchState() (bool, string) {
	if s.app == nil {
		return false, ""
	}
	s.app.BackendStateMu().Lock()
	defer s.app.BackendStateMu().Unlock()
	return s.app.BackendSwitching(), s.app.BackendSwitchTarget()
}

// BackendSwitchBlockedReasonForTraffic returns a reason string if a backend
// switch is in progress, blocking traffic.
func (s RuntimeStateService) BackendSwitchBlockedReasonForTraffic() string {
	switching, target := s.BackendSwitchState()
	if !switching {
		return ""
	}
	if display := BackendDisplayName(strings.TrimSpace(target)); display != "" {
		return "当前正在切换到 " + display + " backend，请稍后再试"
	}
	return "当前正在切换 backend，请稍后再试"
}

// BackendSwitchBlocksCardAction returns a reason string if a backend switch
// blocks the given card action.
func (s RuntimeStateService) BackendSwitchBlocksCardAction(actionName string) string {
	if strings.TrimSpace(actionName) == "menu.backend.switch" {
		return ""
	}
	return s.BackendSwitchBlockedReasonForTraffic()
}
