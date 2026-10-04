package feishuapp

import (
	"context"
	"feidex/internal/application/continuation"
	domainsubmission "feidex/internal/domain/submission"
	frontendruntime "feidex/internal/runtime"
	"feidex/internal/state"
	"fmt"
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

func recordMessageLink(a *App, messageID, kind string, sub *domainsubmission.Submission, requestID string) {
	if sub == nil {
		return
	}
	recordMessageLinkForAnchor(a, messageID, kind, anchorForSubmission(sub), requestID)
}

// recordMessageLinkForAnchor records the link for an already resolved anchor,
// which may have no submission behind it (detached interactive cards).
func recordMessageLinkForAnchor(a *App, messageID, kind string, anchor pendingCardAnchor, requestID string) {
	if a == nil {
		return
	}
	newMessageLinkRecorder(a.configView(), a.runtimeOwner, a.bindings.Continuation).Record(messageID, kind, anchor, requestID)
}

func sendLocalTurnFollowupCard(ctx context.Context, a *App, parentMessageID string, card map[string]any, replyInThread bool, sub *domainsubmission.Submission, kind string) (string, error) {
	if a == nil || a.feishu == nil {
		return "", fmt.Errorf("follow-up unavailable")
	}
	parentMessageID = strings.TrimSpace(parentMessageID)
	if parentMessageID == "" {
		return "", fmt.Errorf("follow-up parent message missing")
	}
	if card == nil {
		return "", fmt.Errorf("follow-up card unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	messageID, err := replyCardWithIDEffect(ctx, a, parentMessageID, card, replyInThread)
	if err != nil {
		return "", err
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", fmt.Errorf("follow-up message id missing")
	}
	if sub != nil && strings.TrimSpace(sub.SessionKey) != "" && strings.TrimSpace(sub.ThreadID) != "" && strings.TrimSpace(sub.TurnID) != "" {
		recordMessageLink(a, messageID, kind, sub, "")
	}
	return messageID, nil
}
