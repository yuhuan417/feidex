package feishuapp

import (
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/domain/conversation"
	"feidex/internal/state"
)

type maintenanceRepository struct {
	store      *appstate.Store
	frontendID string
}

func (r maintenanceRepository) Sessions() []*conversation.Session {
	var result []*conversation.Session
	for _, sess := range r.store.Sessions() {
		if sess != nil && sessionBelongsToFrontend(r.frontendID, sess.Key) {
			result = append(result, sess)
		}
	}
	return result
}
func (r maintenanceRepository) PendingRequests() []*state.PendingRequest {
	return r.store.PendingRequests()
}
func MaintenanceRepository(store *appstate.Store, frontendID string) backendmaintenance.MaintenanceRepository {
	return maintenanceRepository{store: store, frontendID: frontendID}
}
