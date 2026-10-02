package appcore

import (
	"feidex/internal/state"
	"strings"
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
