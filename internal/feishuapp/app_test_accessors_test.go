package feishuapp

import domainbackend "feidex/internal/domain/backend"

// These test-only conveniences keep fixture setup readable without retaining
// compatibility facades in the production App API.
func (a *App) Backend() string {
	if a == nil || a.runtimeOwner == nil {
		return ""
	}
	return a.runtimeOwner.Backend()
}

func (a *App) SetBackend(backend string) {
	if a == nil || a.runtimeOwner == nil {
		return
	}
	a.runtimeOwner.SetBackend(domainbackend.NormalizeBackend(backend))
	if a.stateView != nil {
		a.stateView.SetBackend(a.runtimeOwner.Backend())
	}
}
