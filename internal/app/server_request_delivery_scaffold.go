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
	linkKind        string
	ttl             time.Duration
}

func (a *App) deliverPendingCard(sub *state.Submission, card map[string]any, delivery pendingCardDelivery) error {
	if a == nil || a.feishu == nil || sub == nil {
		return fmt.Errorf("pending card delivery unavailable")
	}
	requestKey := strings.TrimSpace(delivery.requestKey)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}
	waitingStatus := strings.TrimSpace(delivery.waitingStatus)
	if waitingStatus == "" {
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
	msgID, err := a.feishu.SendCard(context.Background(), sub.ChatID, card)
	if err != nil {
		return err
	}
	now := time.Now()
	recordMessageLink(a, msgID, linkKind, sub, requestKey)
	_ = appState(a).savePending(&state.PendingRequest{
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
		Status:       "pending",
		CreatedAt:    now.Unix(),
		ExpiresAt:    now.Add(ttl).Unix(),
	})
	_ = appState(a).setSubmissionStatus(sub.ID, waitingStatus)
	return nil
}
