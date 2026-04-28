// Package appstate centralizes frontend-scoped access to the persistent
// state store for the app layer.
package appstate

import (
	"strings"

	"feidex/internal/app/appcore"
	"feidex/internal/app/sessionctx"
	"feidex/internal/state"
)

// Store provides frontend-scoped access to state.Store.
type Store struct {
	appcore.AppStateFacade
}

// New creates a frontend-scoped Store from an app config host.
func New(a appcore.AppConfig) *Store {
	if a == nil {
		return nil
	}
	return &Store{AppStateFacade: *appcore.NewAppState(a)}
}

func cloneSession(sess *state.Session) *state.Session {
	return appcore.StateCloneSession(sess)
}

// Session returns a session by key.
func (s *Store) Session(key string) *state.Session {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.GetSession(strings.TrimSpace(key))
}

// Sessions returns all sessions.
func (s *Store) Sessions() []*state.Session {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.AllSessions()
}

// SaveSession persists a session snapshot.
func (s *Store) SaveSession(sess *state.Session) error {
	if s == nil || s.Store == nil || sess == nil {
		return nil
	}
	cp := cloneSession(sess)
	if cp == nil {
		return nil
	}
	if s.Backend != "" {
		sessionctx.StoreBackendThread(cp, s.Backend)
	}
	return s.Store.UpsertSession(cp)
}

// UpdateSession mutates and persists a session.
func (s *Store) UpdateSession(key string, mutate func(*state.Session)) (*state.Session, error) {
	if s == nil || s.Store == nil {
		return nil, nil
	}
	return s.Store.UpdateSession(strings.TrimSpace(key), func(sess *state.Session) {
		if mutate != nil {
			mutate(sess)
		}
		if s.Backend != "" {
			sessionctx.StoreBackendThread(sess, s.Backend)
		}
	})
}
