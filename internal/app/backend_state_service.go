package app

import (
	"feidex/internal/app/backend"
)

// runtimeStateService wraps backend.RuntimeStateService to preserve the
// lowercase method names used throughout app/.
type runtimeStateService struct {
	app   *App
	inner backend.RuntimeStateService
}

func newRuntimeStateService(app *App) runtimeStateService {
	return runtimeStateService{
		app:   app,
		inner: backend.NewRuntimeStateService(app),
	}
}

func (s runtimeStateService) beginBackendSwitchState(target string) {
	s.inner.BeginBackendSwitchState(target)
}

func (s runtimeStateService) finishBackendSwitchState() {
	s.inner.FinishBackendSwitchState()
}

func (s runtimeStateService) backendSwitchState() (bool, string) {
	return s.inner.BackendSwitchState()
}

func (s runtimeStateService) backendSwitchBlockedReasonForTraffic() string {
	return s.inner.BackendSwitchBlockedReasonForTraffic()
}

func (s runtimeStateService) backendSwitchBlocksCardAction(actionName string) string {
	return s.inner.BackendSwitchBlocksCardAction(actionName)
}

// Frontend traffic methods — these access App fields directly.
func (s runtimeStateService) beginFrontendMessageTraffic() {
	if s.app == nil {
		return
	}
	s.app.frontendTrafficMu.Lock()
	defer s.app.frontendTrafficMu.Unlock()
	s.app.frontendMessageTraffic++
}

func (s runtimeStateService) finishFrontendMessageTraffic() {
	if s.app == nil {
		return
	}
	s.app.frontendTrafficMu.Lock()
	defer s.app.frontendTrafficMu.Unlock()
	if s.app.frontendMessageTraffic > 0 {
		s.app.frontendMessageTraffic--
	}
}

func (s runtimeStateService) frontendMessageTrafficCount() int {
	if s.app == nil {
		return 0
	}
	s.app.frontendTrafficMu.Lock()
	defer s.app.frontendTrafficMu.Unlock()
	return s.app.frontendMessageTraffic
}

// backendDisplayName wraps backend.BackendDisplayName.
func backendDisplayName(bk string) string {
	return backend.BackendDisplayName(bk)
}
