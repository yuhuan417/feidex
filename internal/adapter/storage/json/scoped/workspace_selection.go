package appstate

import (
	"feidex/internal/application/routing"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	domainrouting "feidex/internal/domain/routing"
)

// WorkspaceSelections stores backend-independent selection records; the
// conversation repository separately owns backend lineage and turn state.
type WorkspaceSelections struct{ *Store }

func (s WorkspaceSelections) Session(key string) *conversation.Session {
	if s.Store == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().GetSession(key)
}
func (s WorkspaceSelections) SaveSession(sess *conversation.Session) error {
	if s.Store == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().UpsertSession(sess)
}
func (s WorkspaceSelections) SetProfileWorkspace(workspaceID string) error {
	if s.Store == nil || s.stateStore() == nil {
		return nil
	}
	service := routing.ConfigurationService{Repository: s.Store, Frontend: identity.FrontendID(s.scopeFrontendID())}
	_, err := service.UpdateProfile(func(profile *domainrouting.BotProfile) { profile.WorkspaceID = workspaceID })
	return err
}
