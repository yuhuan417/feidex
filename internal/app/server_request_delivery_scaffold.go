package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/state"
)

type pendingCardDelivery struct {
	requestKey      string
	requestIDStored string
	backend         string
	kind            string
	sessionKey      string
	threadID        string
	turnID          string
	itemID          string
	ownerUserID     string
	payloadJSON     string
	waitingStatus   string
	nonBlocking     bool
	reuseMessageID  string
	linkKind        string
	ttl             time.Duration
}

func deliverPendingCard(a *App, sub *state.Submission, card map[string]any, delivery pendingCardDelivery) error {
	if a == nil || a.feishu == nil || sub == nil {
		return fmt.Errorf("pending card delivery unavailable")
	}
	requestKey := strings.TrimSpace(delivery.requestKey)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}
	waitingStatus := strings.TrimSpace(delivery.waitingStatus)
	if waitingStatus == "" && !delivery.nonBlocking {
		return fmt.Errorf("missing waiting status")
	}
	linkKind := strings.TrimSpace(delivery.linkKind)
	if linkKind == "" {
		return fmt.Errorf("missing link kind")
	}
	ttl := delivery.ttl
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	ctx := context.Background()
	msgID := ""
	var err error
	reuseMessageID := strings.TrimSpace(delivery.reuseMessageID)
	if reuseMessageID == "" {
		reuseMessageID = newTurnStreamService(a).takeReasoningOnlyWorkingMessageID(delivery.turnID)
	}
	if reuseMessageID != "" {
		if patchErr := a.feishu.PatchCard(ctx, reuseMessageID, card); patchErr == nil {
			msgID = reuseMessageID
		}
	}
	if msgID == "" {
		if triggerMessageID := strings.TrimSpace(sub.TriggerMessageID); triggerMessageID != "" {
			msgID, err = a.feishu.ReplyCard(ctx, triggerMessageID, card, replyInThreadForSubmission(a, sub))
		}
	}
	if err != nil || strings.TrimSpace(msgID) == "" {
		msgID, err = a.feishu.SendCard(ctx, sub.ChatID, card)
		if err != nil {
			return err
		}
	}
	now := time.Now()
	// The delivered card is the newest card now, so retire the working card:
	// progress that resumes after this request is answered must start a new card
	// rather than patch a card the user has already scrolled past.
	newTurnStreamService(a).discardWorkingCard(delivery.turnID)
	recordMessageLink(a, msgID, linkKind, sub, requestKey)
	if err := a.State().SavePending(&state.PendingRequest{
		ID:           requestKey,
		RequestIDRaw: strings.TrimSpace(delivery.requestIDStored),
		Backend:      normalizeRuntimeBackend(delivery.backend),
		Kind:         strings.TrimSpace(delivery.kind),
		SessionKey:   strings.TrimSpace(delivery.sessionKey),
		ThreadID:     strings.TrimSpace(delivery.threadID),
		TurnID:       strings.TrimSpace(delivery.turnID),
		ItemID:       strings.TrimSpace(delivery.itemID),
		OwnerUserID:  strings.TrimSpace(delivery.ownerUserID),
		FeishuMsgID:  msgID,
		PayloadJSON:  delivery.payloadJSON,
		Status:       state.PendingRequestStatusPending.String(),
		CreatedAt:    now.Unix(),
		ExpiresAt:    now.Add(ttl).Unix(),
	}); err != nil {
		return err
	}
	if waitingStatus != "" {
		return a.State().SetSubmissionStatus(sub.ID, waitingStatus)
	}
	return nil
}
