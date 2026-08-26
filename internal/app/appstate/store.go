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
	resolved := s.resolveSessionKey(key)
	return s.Store.GetSession(resolved)
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
	cp.Key = s.canonicalSessionKey(cp.Key)
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
	return s.Store.UpdateSession(s.resolveSessionKey(key), func(sess *state.Session) {
		if mutate != nil {
			mutate(sess)
		}
		sess.Key = s.canonicalSessionKey(sess.Key)
		if s.Backend != "" {
			sessionctx.StoreBackendThread(sess, s.Backend)
		}
	})
}

func (s *Store) canonicalSessionKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.Contains(key, ":workspace:") || strings.Contains(key, ":pending:") {
		return key
	}
	frontendID, _, _, _, _ := appcore.ParseSessionKey(key)
	if frontendID == "" && s.LegacyFallback {
		frontendID = strings.TrimSpace(s.FrontendID)
	}
	return appcore.CanonicalSessionKey(frontendID, key)
}

func (s *Store) resolveSessionKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || s == nil || s.Store == nil {
		return key
	}
	canonical := s.canonicalSessionKey(key)
	if canonical != "" && canonical != key {
		if sess := s.Store.GetSession(canonical); sess != nil {
			return canonical
		}
		if sess := s.Store.GetSession(key); sess != nil {
			return s.promoteSessionAlias(sess, canonical)
		}
	} else if sess := s.Store.GetSession(key); sess != nil {
		return key
	}
	if canonical != "" && canonical != key {
		if sess := s.Store.GetSession(canonical); sess != nil {
			return canonical
		}
	}
	for _, sess := range s.Store.AllSessions() {
		if sess == nil || strings.Contains(strings.TrimSpace(sess.Key), ":workspace:") || strings.Contains(strings.TrimSpace(sess.Key), ":pending:") {
			continue
		}
		if s.canonicalSessionKey(sess.Key) == canonical {
			return s.promoteSessionAlias(sess, canonical)
		}
	}
	return firstNonEmpty(canonical, key)
}

func (s *Store) promoteSessionAlias(sess *state.Session, canonical string) string {
	canonical = strings.TrimSpace(canonical)
	if sess == nil || canonical == "" || s == nil || s.Store == nil {
		return canonical
	}
	cp := cloneSession(sess)
	if cp == nil {
		return canonical
	}
	cp.Key = canonical
	_ = s.Store.UpsertSession(cp)
	return canonical
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
