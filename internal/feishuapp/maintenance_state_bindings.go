package feishuapp

import (
	"feidex/internal/application/backendmaintenance"
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
func MaintenanceRepository(a *App) backendmaintenance.MaintenanceRepository {
	return maintenanceRepository{app: a}
}
