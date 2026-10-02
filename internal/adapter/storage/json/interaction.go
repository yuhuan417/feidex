package json

import (
	"os"
	"strings"

	"feidex/internal/domain/interaction"
	"feidex/internal/state"
)

// InteractionRepository adapts transient pending-request DTOs in state.Store
// to the interaction use case. It never modifies answer or protocol payloads.
type InteractionRepository struct {
	Store          *state.Store
	FrontendID     string
	LegacyFallback bool
}

func (r InteractionRepository) UpdateRequest(id string, transition func(interaction.Request) interaction.Request) (*interaction.Request, error) {
	id = strings.TrimSpace(id)
	if r.Store == nil || id == "" {
		return nil, nil
	}
	var result interaction.Request
	mutate := func(req *state.PendingRequest) {
		result = transition(requestFromDTO(req))
		req.Status = result.Status
	}
	err := r.Store.UpdateScopedPending(r.FrontendID, id, mutate)
	if err != nil && r.LegacyFallback && r.FrontendID != "" {
		err = r.Store.UpdatePending(id, mutate)
	}
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r InteractionRepository) Requests() []interaction.Request {
	if r.Store == nil {
		return nil
	}
	var result []interaction.Request
	for _, req := range r.Store.AllPendingRequests() {
		if req == nil {
			continue
		}
		frontendID := strings.TrimSpace(req.FrontendID)
		if frontendID == r.FrontendID || (frontendID == "" && r.LegacyFallback) {
			result = append(result, requestFromDTO(req))
		}
	}
	return result
}

func requestFromDTO(req *state.PendingRequest) interaction.Request {
	return interaction.Request{
		ID: req.ID, Backend: req.Backend, Kind: req.Kind, Status: req.Status,
		SessionKey: req.SessionKey, ThreadID: req.ThreadID, TurnID: req.TurnID,
	}
}
