package feishuapp

import (
	"context"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/state"
	"fmt"
	"strings"
)

func recordMessageLink(a *App, messageID, kind string, sub *domainsubmission.Submission, requestID string) {
	if sub == nil {
		return
	}
	recordMessageLinkForAnchor(a, messageID, kind, anchorForSubmission(a, sub), requestID)
}

// recordMessageLinkForAnchor records the link for an already resolved anchor,
// which may have no submission behind it (detached interactive cards).
func recordMessageLinkForAnchor(a *App, messageID, kind string, anchor pendingCardAnchor, requestID string) {
	if strings.TrimSpace(messageID) == "" {
		return
	}
	link := &state.MessageLink{
		Backend:      configuredBackend(a),
		MessageID:    messageID,
		SessionKey:   anchor.sessionKey,
		SubmissionID: anchor.submissionID,
		ThreadID:     anchor.threadID,
		TurnID:       anchor.turnID,
	}
	_ = a.bindings.Continuation.RecordReplyMessageLink(*link)
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
