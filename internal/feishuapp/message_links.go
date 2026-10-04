package feishuapp

import (
	"feidex/internal/application/continuation"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"
	"strings"
)

type messageLinkRecorder struct {
	view         frontendConfigView
	runtimeOwner *frontendruntime.FrontendOwner
	continuation *continuation.Service
}

func (r messageLinkRecorder) Record(messageID, kind string, anchor pendingCardAnchor, requestID string) {
	if strings.TrimSpace(messageID) == "" || r.continuation == nil {
		return
	}
	view := r.view
	if r.runtimeOwner != nil {
		view.backend = r.runtimeOwner.Backend()
	}
	link := &state.MessageLink{
		Backend:      view.configuredBackend(),
		MessageID:    messageID,
		SessionKey:   anchor.sessionKey,
		SubmissionID: anchor.submissionID,
		ThreadID:     anchor.threadID,
		TurnID:       anchor.turnID,
	}
	_ = r.continuation.RecordReplyMessageLink(*link)
}

func newMessageLinkRecorder(view frontendConfigView, runtimeOwner *frontendruntime.FrontendOwner, continuation *continuation.Service) messageLinkRecorder {
	return messageLinkRecorder{view: view, runtimeOwner: runtimeOwner, continuation: continuation}
}
