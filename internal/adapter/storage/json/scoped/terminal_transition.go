package appstate

import (
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/submission"
	"fmt"
)

func (s *Store) CommitTerminal(expectedSession, nextSession *conversation.Session, expectedSubmission, nextSubmission *submission.Submission, expectedRequests, nextRequests []*interaction.PendingRequest) error {
	if expectedSession == nil {
		return fmt.Errorf("foreign terminal session")
	}
	if nextSession == nil || nextSession.Key != expectedSession.Key || expectedSubmission == nil || nextSubmission == nil || expectedSubmission.SessionKey != expectedSession.Key || nextSubmission.SessionKey != expectedSession.Key {
		return fmt.Errorf("foreign terminal owner")
	}
	frontendID, _, _, _, _ := identity.ParseSessionKey(expectedSession.Key)
	if !s.matchesFrontend(frontendID) {
		return fmt.Errorf("foreign terminal session")
	}
	for _, req := range expectedRequests {
		if req == nil || !s.matchesFrontend(req.FrontendID) {
			return fmt.Errorf("foreign terminal interaction")
		}
	}
	for _, req := range nextRequests {
		if req == nil || !s.matchesFrontend(req.FrontendID) {
			return fmt.Errorf("foreign terminal interaction")
		}
	}
	return s.stateStore().CommitTerminal(expectedSession, nextSession, expectedSubmission, nextSubmission, expectedRequests, nextRequests)
}
