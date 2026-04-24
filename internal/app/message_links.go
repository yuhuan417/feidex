package app

import (
	"strings"

	"feidex/internal/state"
)

func recordMessageLink(a *App, messageID, kind string, sub *state.Submission, requestID string) {
	if strings.TrimSpace(messageID) == "" {
		return
	}
	link := &state.MessageLink{
		Backend:   configuredBackend(a),
		MessageID: messageID,
	}
	if sub != nil {
		link.SessionKey = sub.SessionKey
		link.SubmissionID = sub.ID
		link.ThreadID = sub.ThreadID
		link.TurnID = sub.TurnID
	}
	_ = appState(a).saveMessageLink(link)
}
