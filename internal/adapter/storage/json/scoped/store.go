// Package appstate centralizes frontend-scoped access to the persistent
// state store for the app layer.
package appstate

import (
	"strings"
	"sync"

	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/state"
)

// Store provides frontend-scoped access to state.Store.
type Store struct {
	RevisionMutex  sync.Locker
	revisionMu     sync.Mutex
	scopeMu        sync.RWMutex
	store          *state.Store
	frontendID     string
	backend        string
	legacyFallback bool
}

// NewScoped creates a frontend-scoped state gateway from explicit runtime
// dependencies. Keeping construction here makes the scope visible at the
// composition root and avoids a second app-level state facade.
func NewScoped(store *state.Store, frontendID, backend string, legacyFallback bool) *Store {
	return &Store{
		store:          store,
		frontendID:     strings.TrimSpace(frontendID),
		backend:        domainbackend.NormalizeBackend(backend),
		legacyFallback: legacyFallback,
	}
}

func (s *Store) stateStore() *state.Store {
	if s == nil {
		return nil
	}
	if s.store != nil {
		return s.store
	}
	return s.store
}

// StateStore exposes the persistence port to adapters that need to compose a
// lower-level repository. Domain and application code should prefer the
// scoped methods on Store.
func (s *Store) StateStore() *state.Store { return s.stateStore() }

// FrontendID returns the frontend scope owned by this gateway.
func (s *Store) FrontendID() string { return s.scopeFrontendID() }

// Backend returns the normalized backend scope owned by this gateway.
func (s *Store) Backend() string { return s.scopeBackend() }

// LegacyFallbackEnabled reports whether unscoped legacy records are visible.
func (s *Store) LegacyFallbackEnabled() bool { return s.scopeLegacyFallback() }

// SetBackend updates the backend scope after a runtime backend switch.
func (s *Store) SetBackend(backend string) {
	if s == nil {
		return
	}
	s.scopeMu.Lock()
	defer s.scopeMu.Unlock()
	s.backend = domainbackend.NormalizeBackend(backend)
}

func (s *Store) scopeFrontendID() string {
	if s == nil {
		return ""
	}
	if s.frontendID != "" {
		return s.frontendID
	}
	return s.frontendID
}

func (s *Store) scopeBackend() string {
	if s == nil {
		return ""
	}
	s.scopeMu.RLock()
	defer s.scopeMu.RUnlock()
	return s.backend
}

func (s *Store) scopeLegacyFallback() bool {
	if s == nil {
		return false
	}
	if s.legacyFallback {
		return true
	}
	return s.legacyFallback
}

func (s *Store) matchesFrontend(frontendID string) bool {
	frontendID = strings.TrimSpace(frontendID)
	if frontendID == strings.TrimSpace(s.scopeFrontendID()) {
		return true
	}
	return frontendID == "" && s.scopeLegacyFallback()
}

func cloneSession(sess *conversation.Session) *conversation.Session {
	return conversation.CloneSession(sess)
}

// Session returns a session by key.
func (s *Store) Session(key string) *conversation.Session {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	resolved := s.resolveSessionKey(key)
	return s.stateStore().GetSession(resolved)
}

// Sessions returns all sessions.
func (s *Store) Sessions() []*conversation.Session {
	if s == nil || s.stateStore() == nil {
		return nil
	}
	return s.stateStore().AllSessions()
}

// SaveSession persists a session snapshot.
func (s *Store) SaveSession(sess *conversation.Session) error {
	if s == nil || s.stateStore() == nil || sess == nil {
		return nil
	}
	cp := cloneSession(sess)
	if cp == nil {
		return nil
	}
	cp.Key = s.canonicalSessionKey(cp.Key)
	if s.scopeBackend() != "" {
		conversation.StoreBackendThread(cp, s.scopeBackend())
	}
	return s.stateStore().UpsertSession(cp)
}

// UpdateSession mutates and persists a session.
func (s *Store) UpdateSession(key string, mutate func(*conversation.Session)) (*conversation.Session, error) {
	if s == nil || s.stateStore() == nil {
		return nil, nil
	}
	return s.stateStore().UpdateSession(s.resolveSessionKey(key), func(sess *conversation.Session) {
		if mutate != nil {
			mutate(sess)
		}
		sess.Key = s.canonicalSessionKey(sess.Key)
		if s.scopeBackend() != "" {
			conversation.StoreBackendThread(sess, s.scopeBackend())
		}
	})
}

func (s *Store) canonicalSessionKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.Contains(key, ":workspace:") || strings.Contains(key, ":pending:") {
		return key
	}
	frontendID, _, _, _, _ := identity.ParseSessionKey(key)
	if frontendID == "" && s.scopeLegacyFallback() {
		frontendID = strings.TrimSpace(s.scopeFrontendID())
	}
	return identity.CanonicalSessionKey(frontendID, key)
}

func (s *Store) resolveSessionKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || s == nil || s.stateStore() == nil {
		return key
	}
	canonical := s.canonicalSessionKey(key)
	if canonical != "" && canonical != key {
		if sess := s.stateStore().GetSession(canonical); sess != nil {
			return canonical
		}
		if sess := s.stateStore().GetSession(key); sess != nil {
			return s.promoteSessionAlias(sess, canonical)
		}
	} else if sess := s.stateStore().GetSession(key); sess != nil {
		return key
	}
	if canonical != "" && canonical != key {
		if sess := s.stateStore().GetSession(canonical); sess != nil {
			return canonical
		}
	}
	for _, sess := range s.stateStore().AllSessions() {
		if sess == nil || strings.Contains(strings.TrimSpace(sess.Key), ":workspace:") || strings.Contains(strings.TrimSpace(sess.Key), ":pending:") {
			continue
		}
		if s.canonicalSessionKey(sess.Key) == canonical {
			return s.promoteSessionAlias(sess, canonical)
		}
	}
	return firstNonEmpty(canonical, key)
}

func (s *Store) promoteSessionAlias(sess *conversation.Session, canonical string) string {
	canonical = strings.TrimSpace(canonical)
	if sess == nil || canonical == "" || s == nil || s.stateStore() == nil {
		return canonical
	}
	cp := cloneSession(sess)
	if cp == nil {
		return canonical
	}
	cp.Key = canonical
	_ = s.stateStore().UpsertSession(cp)
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

func (s *Store) revisionMutex() sync.Locker {
	if s.RevisionMutex != nil {
		return s.RevisionMutex
	}
	return &s.revisionMu
}
