package app

import (
	"context"
	"fmt"
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
	_ = a.State().SaveMessageLink(link)
}

func sendLocalTurnFollowupCard(ctx context.Context, a *App, parentMessageID string, card map[string]any, replyInThread bool, sub *state.Submission, kind string) (string, error) {
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
	messageID, err := a.feishu.ReplyCard(ctx, parentMessageID, card, replyInThread)
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
