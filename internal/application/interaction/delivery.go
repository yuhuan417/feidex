package interaction

import (
	"context"
	"fmt"
	"strings"
	"time"

	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
)

type PendingRepository interface {
	Pending(string) *interaction.PendingRequest
	SavePending(*interaction.PendingRequest) error
	UpdatePending(string, func(*interaction.PendingRequest)) error
	SetSubmissionStatus(string, string) error
	SaveMessageLink(*conversation.MessageLink) error
}

type DeliveryInput struct {
	Request       interaction.PendingRequest
	SubmissionID  string
	WaitingStatus string
	NonBlocking   bool
	TTL           time.Duration
}

type InteractionPresenter interface {
	DeliverInteraction(context.Context, DeliveryInput) (string, error)
}

type DeliveryService struct{ Repository PendingRepository }

// Open persists the request and its waiting boundary before external delivery.
// Retrying an undelivered request retains its identity and original lifetime.
func (s DeliveryService) Open(ctx context.Context, input DeliveryInput, presenter InteractionPresenter) error {
	req := input.Request
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		return fmt.Errorf("missing request id")
	}
	input.WaitingStatus = strings.TrimSpace(input.WaitingStatus)
	if input.WaitingStatus == "" && !input.NonBlocking {
		return fmt.Errorf("missing waiting status")
	}
	if existing := s.Repository.Pending(req.ID); existing != nil {
		// A repeated backend event must never reopen a replied or terminal request.
		if strings.TrimSpace(existing.Status) != "pending" {
			return nil
		}
		if existing.ExpiresAt > 0 && existing.ExpiresAt <= time.Now().Unix() {
			return s.Repository.UpdatePending(req.ID, func(current *interaction.PendingRequest) { current.Status = "expired" })
		}
		req = *existing
	} else {
		if input.TTL <= 0 {
			input.TTL = 30 * time.Minute
		}
		now := time.Now()
		req.Status, req.CreatedAt, req.ExpiresAt = "pending", now.Unix(), now.Add(input.TTL).Unix()
		if err := s.Repository.SavePending(&req); err != nil {
			return err
		}
	}
	if input.WaitingStatus != "" && input.SubmissionID != "" {
		if err := s.Repository.SetSubmissionStatus(input.SubmissionID, input.WaitingStatus); err != nil {
			return err
		}
	}
	input.Request = req
	id := strings.TrimSpace(req.FeishuMsgID)
	if id == "" {
		var err error
		id, err = presenter.DeliverInteraction(ctx, input)
		if err != nil {
			return err
		}
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("interaction delivery returned empty message id")
		}
		if err := s.Repository.UpdatePending(req.ID, func(current *interaction.PendingRequest) { current.FeishuMsgID = id }); err != nil {
			return err
		}
	}
	return s.Repository.SaveMessageLink(&conversation.MessageLink{
		Backend: req.Backend, MessageID: id, SessionKey: req.SessionKey, SubmissionID: input.SubmissionID, ThreadID: req.ThreadID, TurnID: req.TurnID,
	})
}
