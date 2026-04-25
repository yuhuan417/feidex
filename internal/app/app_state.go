package app

import (
	"strings"

	"feidex/internal/state"
)

// appStateFacade centralizes app-level access to state.Store so lifecycle
// code can depend on a narrower surface before state is split further.
type appStateFacade struct {
	store          *state.Store
	frontendID     string
	backend        string
	legacyFallback bool
}

func appState(a *App) *appStateFacade {
	return &appStateFacade{
		store:          a.store,
		frontendID:     strings.TrimSpace(a.frontendID),
		backend:        normalizeRuntimeBackend(configuredBackend(a)),
		legacyFallback: allowLegacyFrontendFallback(a),
	}
}

func (s *appStateFacade) matchesFrontend(frontendID string) bool {
	frontendID = strings.TrimSpace(frontendID)
	if frontendID == strings.TrimSpace(s.frontendID) {
		return true
	}
	return frontendID == "" && s.legacyFallback
}

func stateCloneSession(sess *state.Session) *state.Session {
	if sess == nil {
		return nil
	}
	cp := *sess
	cp.Queue = append([]string(nil), sess.Queue...)
	cp.ActiveOperations = append([]state.SessionActiveOperation(nil), sess.ActiveOperations...)
	cp.StagedImages = append([]state.SessionStagedImage(nil), sess.StagedImages...)
	if len(sess.BackendThreads) > 0 {
		cp.BackendThreads = make(map[string]state.SessionBackendThread, len(sess.BackendThreads))
		for key, value := range sess.BackendThreads {
			cp.BackendThreads[key] = value
		}
	}
	return &cp
}
