package lifecycle

import (
	"feidex/internal/domain/interaction"
	"feidex/internal/state"
)

// These DTO adaptations keep persistence outside the interaction domain.
func IsPendingRequestOpen(req *state.PendingRequest) bool {
	return req != nil && interaction.IsPendingRequestOpen(req.Status)
}

func OutlivesTurn(req *state.PendingRequest) bool {
	return req != nil && interaction.OutlivesTurn(req.Backend, req.Kind, req.Status)
}
