package app

import (
	"feidex/internal/adapter/feishu/backend"
	"feidex/internal/domain/conversation"
	"feidex/internal/state"
)

type maintenanceRepository struct{ app *App }

func (r maintenanceRepository) Sessions() []*conversation.Session {
	var result []*conversation.Session
	for _, sess := range r.app.State().Sessions() {
		if sess != nil && sessionBelongsToFrontend(r.app, sess.Key) {
			result = append(result, sess)
		}
	}
	return result
}
func (r maintenanceRepository) PendingRequests() []*state.PendingRequest {
	return r.app.State().PendingRequests()
}
func newMaintenanceStateService(a *App) backend.MaintenanceStateService {
	if a == nil {
		return backend.MaintenanceStateService{}
	}
	return backend.NewMaintenanceStateService(a.MaintenanceTrackers(), maintenanceRepository{app: a})
}
