package app

import (
	"strings"

	"feidex/internal/state"
)

func (s *appStateFacade) session(key string) *state.Session {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.GetSession(strings.TrimSpace(key))
}

func (s *appStateFacade) sessions() []*state.Session {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.AllSessions()
}

func (s *appStateFacade) saveSession(sess *state.Session) error {
	if s == nil || s.store == nil || sess == nil {
		return nil
	}
	cp := stateCloneSession(sess)
	if cp == nil {
		return nil
	}
	if s.backend != "" {
		sessionStoreBackendThread(cp, s.backend)
	}
	return s.store.UpsertSession(cp)
}

func (s *appStateFacade) updateSession(key string, mutate func(*state.Session)) (*state.Session, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.UpdateSession(strings.TrimSpace(key), func(sess *state.Session) {
		if mutate != nil {
			mutate(sess)
		}
		if s.backend != "" {
			sessionStoreBackendThread(sess, s.backend)
		}
	})
}
