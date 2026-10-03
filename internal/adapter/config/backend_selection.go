package config

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/state"
)

type BackendSelectionSource interface {
	BackendSource
	FrontendID() string
	Backend() string
	Store() *state.Store
}

type BackendSelectionRepository struct {
	Source     BackendSelectionSource
	Configured func() string
}

func (r BackendSelectionRepository) ConfiguredBackend() string { return r.Configured() }
func (r BackendSelectionRepository) Sessions() []*conversation.Session {
	if r.Source.Store() == nil {
		return nil
	}
	var out []*conversation.Session
	for _, sess := range r.Source.Store().AllSessions() {
		if sess == nil {
			continue
		}
		frontend, _, _, _, _ := identity.ParseSessionKey(sess.Key)
		if frontend == r.Source.FrontendID() {
			out = append(out, sess)
		}
	}
	return out
}
func (r BackendSelectionRepository) CommitBackend(target string, expected, next []*conversation.Session) error {
	r.Source.ConfigMu().Lock()
	defer r.Source.ConfigMu().Unlock()
	persist := func() error { return NewBackendRepository(r.Source).setBackendLocked(target) }
	if r.Source.Store() == nil {
		return persist()
	}
	return r.Source.Store().CommitBackendSessions(expected, next, persist)
}
