package app

import (
	appreviewcmd "feidex/internal/app/reviewcmd"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Wrapper type — delegates to reviewcmd.ReviewFormService
// ---------------------------------------------------------------------------

type reviewFormService struct {
	inner appreviewcmd.ReviewFormService
}

func newReviewFormService(app *App) reviewFormService {
	return reviewFormService{inner: appreviewcmd.NewReviewFormService(app)}
}

func (s reviewFormService) renderReviewMenuCard(sessionKey string) map[string]any {
	return s.inner.RenderReviewMenuCard(sessionKey)
}

func (s reviewFormService) beginReviewForm(msg *feishu.InboundMessage, mode string) error {
	return s.inner.BeginReviewForm(msg, mode)
}

func (s reviewFormService) renderReviewFormCard(sessionKey, requestID string, payload reviewPendingPayload) (map[string]any, error) {
	return s.inner.RenderReviewFormCard(sessionKey, requestID, payload)
}

func (s reviewFormService) renderReviewBaseCard(sessionKey, requestID string, payload reviewPendingPayload) (map[string]any, error) {
	return s.inner.RenderReviewBaseCard(sessionKey, requestID, payload)
}

func (s reviewFormService) renderReviewCommitCard(sessionKey, requestID string, payload reviewPendingPayload) (map[string]any, error) {
	return s.inner.RenderReviewCommitCard(sessionKey, requestID, payload)
}

func (s reviewFormService) renderReviewCustomCard(sessionKey, requestID string, payload reviewPendingPayload) map[string]any {
	return s.inner.RenderReviewCustomCard(sessionKey, requestID, payload)
}

func (s reviewFormService) completeReviewBaseSelect(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteReviewBaseSelect(action)
}

func (s reviewFormService) completeReviewCommitSelect(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteReviewCommitSelect(action)
}

func (s reviewFormService) completeReviewFormSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteReviewFormSubmit(action)
}
