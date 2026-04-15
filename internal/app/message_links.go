package app

import (
	"strings"

	"feidex/internal/state"
)

func (a *App) recordMessageLink(messageID, kind string, sub *state.Submission, requestID string) {
	if strings.TrimSpace(messageID) == "" {
		return
	}
	link := &state.MessageLink{
		MessageID: messageID,
	}
	if sub != nil {
		link.SessionKey = sub.SessionKey
		link.SubmissionID = sub.ID
		link.ThreadID = sub.ThreadID
		link.TurnID = sub.TurnID
	}
	_ = a.appState().saveMessageLink(link)
}
