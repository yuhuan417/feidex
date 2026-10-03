package json

import (
	"feidex/internal/domain/interaction"
	"feidex/internal/state"
)

// IsPendingRequestOpen adapts the persisted pending-request DTO to the
// interaction domain rule. Persistence stays outside the domain package.
func IsPendingRequestOpen(req *state.PendingRequest) bool {
	return req != nil && interaction.IsPendingRequestOpen(req.Status)
}
