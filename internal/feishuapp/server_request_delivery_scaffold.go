package feishuapp

import (
	"context"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	appturnstream "feidex/internal/adapter/feishu/turnstream"
	"feidex/internal/application"
	applicationinteraction "feidex/internal/application/interaction"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/interaction"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/runtime"
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

type pendingWorkingTurnState interface {
	TakeReasoningOnlyWorkingMessageID(string) string
	DiscardWorkingCard(string)
}

// PendingCardDeliveryInputs contains the runtime-owned collaborators used to
// deliver a pending interaction card.
type PendingCardDeliveryInputs struct {
	Interactions *applicationinteraction.DeliveryService
	Lifecycle    *runtime.FrontendRuntime
	Frontend     identity.FrontendID
	Deduper      runtime.EffectDeduper
	Turns        *appturnstream.Service
	Runner       runtime.EffectRunner
	Ready        bool
}

// PendingCardDeliveryService owns the pending-card delivery boundary shared by
// Claude and Codex server requests.
type PendingCardDeliveryService struct {
	interactions *applicationinteraction.DeliveryService
	lifecycle    *runtime.FrontendRuntime
	frontend     identity.FrontendID
	deduper      runtime.EffectDeduper
	turns        pendingWorkingTurnState
	runner       runtime.EffectRunner
	ready        bool
}

func NewPendingCardDeliveryService(inputs PendingCardDeliveryInputs) PendingCardDeliveryService {
	return PendingCardDeliveryService{
		interactions: inputs.Interactions,
		lifecycle:    inputs.Lifecycle,
		frontend:     inputs.Frontend,
		deduper:      inputs.Deduper,
		turns:        inputs.Turns,
		runner:       inputs.Runner,
		ready:        inputs.Ready,
	}
}

func (s PendingCardDeliveryService) Deliver(anchor pendingCardAnchor, card map[string]any, delivery pendingCardDelivery) error {
	if !s.ready || s.lifecycle == nil {
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
	input := applicationinteraction.DeliveryInput{
		Request:      interaction.PendingRequest{ID: requestKey, RequestIDRaw: strings.TrimSpace(delivery.requestIDStored), Backend: normalizeRuntimeBackend(delivery.backend), Kind: strings.TrimSpace(delivery.kind), SessionKey: strings.TrimSpace(delivery.sessionKey), ThreadID: strings.TrimSpace(delivery.threadID), TurnID: strings.TrimSpace(delivery.turnID), ItemID: strings.TrimSpace(delivery.itemID), OwnerUserID: strings.TrimSpace(delivery.ownerUserID), PayloadJSON: delivery.payloadJSON},
		SubmissionID: anchor.submissionID, WaitingStatus: waitingStatus, NonBlocking: delivery.nonBlocking, TTL: ttl,
	}
	return s.interactions.Open(s.lifecycle.Context(), input, pendingCardPresenter{
		frontend: s.frontend, deduper: s.deduper, turns: s.turns, runner: s.runner,
		anchor: anchor, card: card, reuseMessageID: delivery.reuseMessageID,
	})
}

func anchorForSubmission(sub *domainsubmission.Submission) pendingCardAnchor {
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
		replyInThread:    replyInThreadForSubmission(sub),
	}
}

type pendingCardPresenter struct {
	frontend       identity.FrontendID
	deduper        runtime.EffectDeduper
	turns          pendingWorkingTurnState
	runner         runtime.EffectRunner
	anchor         pendingCardAnchor
	card           map[string]any
	reuseMessageID string
}

func (p pendingCardPresenter) DeliverInteraction(ctx context.Context, input applicationinteraction.DeliveryInput) (string, error) {
	reuse := strings.TrimSpace(p.reuseMessageID)
	if reuse == "" {
		reuse = p.turns.TakeReasoningOnlyWorkingMessageID(input.Request.TurnID)
	}
	key := application.StableEffectKey("interaction-card", string(p.frontend), input.Request.ID)
	messageID, err := p.deduper.Do(ctx, key, func() (any, error) {
		if reuse != "" {
			effect := application.PatchCard{
				Frontend: p.frontend, MessageID: reuse, View: feishuoutbound.Card(p.card),
				IdempotencyKey: cardEffectKey("patch-card", string(p.frontend), reuse, p.card),
			}
			if err := p.runner.Run(ctx, []application.Effect{effect}); err == nil {
				return reuse, nil
			}
		}
		if parent := strings.TrimSpace(p.anchor.triggerMessageID); parent != "" {
			id, err := p.runner.RunSendCard(ctx, application.SendCard{
				Frontend: p.frontend, ReplyMessageID: parent,
				View: feishuoutbound.Card(p.card), InThread: p.anchor.replyInThread,
			})
			if err == nil && strings.TrimSpace(id) != "" {
				return id, nil
			}
		}
		return p.runner.RunSendCard(ctx, application.SendCard{
			Frontend: p.frontend, Chat: identity.ChatRef{ID: p.anchor.chatID},
			View: feishuoutbound.Card(p.card),
		})
	})
	if err != nil {
		return "", err
	}
	p.turns.DiscardWorkingCard(input.Request.TurnID)
	id, _ := messageID.(string)
	return id, nil
}
