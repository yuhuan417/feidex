package appstate

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"strings"
)

func (s *Store) DeleteTurnArtifacts(turnID string) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	s.DeletePendingRequests(func(req *interaction.PendingRequest) bool {
		return req != nil && strings.TrimSpace(req.TurnID) == turnID
	})
	s.DeleteMessageLinks(func(link *conversation.MessageLink) bool {
		return link != nil && strings.TrimSpace(link.TurnID) == turnID
	})
}
