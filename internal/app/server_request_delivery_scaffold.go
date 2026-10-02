package app

import (
	"context"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/state"
	"fmt"
	"strings"
	"time"
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

// pendingCardAnchor identifies where a pending card is delivered. A live
// submission is optional: a request from a background agent can outlive the
// submission that produced it and is then delivered against the session's
// durable Feishu anchors instead.
type pendingCardAnchor struct {
	sessionKey       string
	chatID           string
	triggerMessageID string
	submissionID     string
	threadID         string
	turnID           string
	ownerUserID      string
	replyInThread    bool
}

func anchorForSubmission(a *App, sub *domainsubmission.Submission) pendingCardAnchor {
	if sub == nil {
		return pendingCardAnchor{}
	}
	return pendingCardAnchor{
		sessionKey:       strings.TrimSpace(sub.SessionKey),
		chatID:           strings.TrimSpace(sub.ChatID),
		triggerMessageID: strings.TrimSpace(sub.TriggerMessageID),
		submissionID:     strings.TrimSpace(sub.ID),
		threadID:         strings.TrimSpace(sub.ThreadID),
		turnID:           strings.TrimSpace(sub.TurnID),
		ownerUserID:      strings.TrimSpace(sub.UserID),
		replyInThread:    replyInThreadForSubmission(a, sub),
	}
}

func deliverPendingCard(a *App, sub *domainsubmission.Submission, card map[string]any, delivery pendingCardDelivery) error {
	if sub == nil {
		return fmt.Errorf("pending card delivery unavailable")
	}
	return deliverPendingCardWithAnchor(a, anchorForSubmission(a, sub), card, delivery)
}

// deliverDetachedPendingCard delivers a card for a request that outlived its
// producing turn, so there is no submission left to attach it to. The card is
// non-blocking by definition: it must not touch submission status.
func deliverDetachedPendingCard(a *App, anchor pendingCardAnchor, card map[string]any, delivery pendingCardDelivery) error {
	delivery.nonBlocking = true
	return deliverPendingCardWithAnchor(a, anchor, card, delivery)
}

func deliverPendingCardWithAnchor(a *App, anchor pendingCardAnchor, card map[string]any, delivery pendingCardDelivery) error {
	if a == nil || a.feishu == nil {
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
		if patchErr := patchCardEffect(ctx, a, reuseMessageID, card); patchErr == nil {
			msgID = reuseMessageID
		}
	}
	if msgID == "" {
		if triggerMessageID := strings.TrimSpace(anchor.triggerMessageID); triggerMessageID != "" {
			msgID, err = replyCardWithIDEffect(ctx, a, triggerMessageID, card, anchor.replyInThread)
		}
	}
	if err != nil || strings.TrimSpace(msgID) == "" {
		msgID, err = sendCardWithIDEffect(ctx, a, anchor.chatID, card)
		if err != nil {
			return err
		}
	}
	now := time.Now()
	// The delivered card is the newest card now, so retire the working card:
	// progress that resumes after this request is answered must start a new card
	// rather than patch a card the user has already scrolled past.
	newTurnStreamService(a).discardWorkingCard(delivery.turnID)
	recordMessageLinkForAnchor(a, msgID, linkKind, anchor, requestKey)
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
	if waitingStatus != "" && anchor.submissionID != "" {
		return a.State().SetSubmissionStatus(anchor.submissionID, waitingStatus)
	}
	return nil
}
