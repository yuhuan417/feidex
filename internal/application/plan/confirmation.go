package plan

import (
	"context"
	"encoding/json"
	interactionapp "feidex/internal/application/interaction"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"feidex/internal/domain/submission"
	"feidex/internal/textutil"
	"fmt"
	"strings"
	"time"
)

const ConfirmationKind = "codex_exit_plan_mode"

type ConfirmationPayload struct {
	PlanMarkdown string `json:"plan_markdown"`
}
type ConfirmationInput struct {
	SessionKey, ThreadID, TurnID, Status, Markdown string
	Eligible                                       bool
	Submission                                     *submission.Submission
}

func (s Service) Confirmation(key string) *interaction.PendingRequest {
	var latest *interaction.PendingRequest
	for _, req := range s.Repository.PendingRequests() {
		if req != nil && req.Kind == ConfirmationKind && req.SessionKey == strings.TrimSpace(key) && interaction.IsPendingRequestOpen(req.Status) && (latest == nil || req.CreatedAt > latest.CreatedAt) {
			latest = req
		}
	}
	return latest
}
func (s Service) Invalidate(key string) (*interaction.PendingRequest, error) {
	req := s.Confirmation(key)
	if req == nil {
		return nil, nil
	}
	if err := s.Forms.SaveDraft(req.ID, nil, "expired", 0, ""); err != nil {
		return nil, err
	}
	return req, nil
}

// Offer runs only at the terminal turn boundary; blocked offers are not deferred.
func (s Service) Offer(ctx context.Context, input ConfirmationInput, presenter interactionapp.InteractionPresenter) (bool, error) {
	if !input.Eligible || input.Submission == nil || input.Status != "completed" || strings.TrimSpace(input.Markdown) == "" {
		return false, nil
	}
	sub := input.Submission
	key := textutil.FirstNonEmpty(input.SessionKey, sub.SessionKey)
	threadID := textutil.FirstNonEmpty(input.ThreadID, sub.ThreadID)
	sess := s.Repository.Session(key)
	if sess == nil || sess.ActiveThreadID == "" || sess.ActiveThreadID != threadID || conversation.HasActiveWork(sess) || len(sess.Queue) > 0 || len(sess.StagedImages) > 0 || conversation.NormalizeSessionStatus(sess.Status) != conversation.SessionStatusIdle {
		return false, nil
	}
	id := "codex-plan-exit-" + sub.ID
	for _, req := range s.Repository.PendingRequests() {
		if req != nil && req.SessionKey == key && req.ID != id && interaction.IsPendingRequestOpen(req.Status) {
			return false, nil
		}
	}
	if existing := s.Repository.Pending(id); existing != nil && existing.Status != "pending" {
		return false, nil
	}
	payload, err := json.Marshal(ConfirmationPayload{PlanMarkdown: strings.TrimSpace(input.Markdown)})
	if err != nil {
		return false, err
	}
	// A local post-turn chooser must survive cleanup of the completed turn.
	request := interaction.PendingRequest{ID: id, Kind: ConfirmationKind, SessionKey: key, ThreadID: threadID, OwnerUserID: sub.UserID, PayloadJSON: string(payload)}
	err = s.Delivery.Open(ctx, interactionapp.DeliveryInput{Request: request, NonBlocking: true, TTL: 30 * time.Minute}, presenter)
	return err == nil, err
}
func (s Service) Authorize(id, userID string) (*interaction.PendingRequest, error) {
	return s.Forms.Authorize(id, ConfirmationKind, userID)
}
func confirmationMarkdown(req *interaction.PendingRequest) (string, error) {
	var payload ConfirmationPayload
	if err := json.Unmarshal([]byte(req.PayloadJSON), &payload); err != nil {
		return "", fmt.Errorf("invalid plan confirmation: %w", err)
	}
	return payload.PlanMarkdown, nil
}
