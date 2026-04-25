package appcore

import (
	"strings"

	"feidex/internal/state"
)

// AppStateFacade centralizes app-level access to state.Store so lifecycle
// code can depend on a narrower surface before state is split further.
type AppStateFacade struct {
	Store          *state.Store
	FrontendID     string
	Backend        string
	LegacyFallback bool
}

// NewAppState creates an AppStateFacade from an AppConfig.
func NewAppState(a AppConfig) *AppStateFacade {
	return &AppStateFacade{
		Store:          a.Store(),
		FrontendID:     strings.TrimSpace(a.FrontendID()),
		Backend:        NormalizeRuntimeBackend(ConfiguredBackend(a)),
		LegacyFallback: AllowLegacyFrontendFallback(a),
	}
}

// MatchesFrontend returns true if the given frontend ID matches this facade.
func (s *AppStateFacade) MatchesFrontend(frontendID string) bool {
	frontendID = strings.TrimSpace(frontendID)
	if frontendID == strings.TrimSpace(s.FrontendID) {
		return true
	}
	return frontendID == "" && s.LegacyFallback
}

// StateCloneSession creates a deep copy of a session.
func StateCloneSession(sess *state.Session) *state.Session {
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
