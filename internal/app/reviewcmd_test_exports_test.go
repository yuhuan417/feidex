package app

import (
	"context"

	appreview "feidex/internal/app/review"
	appreviewcmd "feidex/internal/app/reviewcmd"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func commandReview(a *App, msg *feishu.InboundMessage, args []string) error {
	return appreviewcmd.CommandReview(newReviewAppAdapter(a), msg, args)
}

func startSubmissionReview(a *App, ctx context.Context, threadID string, sub *state.Submission) (string, error) {
	return appreviewcmd.StartSubmissionReview(newReviewAppAdapter(a), ctx, threadID, sub)
}

type reviewFormService struct {
	inner appreviewcmd.ReviewFormService
}

func newReviewFormService(app *App) reviewFormService {
	return reviewFormService{inner: newReviewFormServiceInner(app)}
}

func (s reviewFormService) beginReviewForm(msg *feishu.InboundMessage, mode string) error {
	return s.inner.BeginReviewForm(msg, mode)
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

type reviewGitService struct{}

func newReviewGitService(_ *App) reviewGitService {
	return reviewGitService{}
}

func (s reviewGitService) resolveReviewTarget(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error) {
	return appreview.NewGitService().ResolveTarget(cwd, target)
}

func completeMenuReviewUncommitted(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return appreviewcmd.CompleteMenuReviewUncommitted(newReviewAppAdapter(a), action, sessionKey)
}

func completeMenuReviewBase(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return appreviewcmd.CompleteMenuReviewBase(newReviewAppAdapter(a), action, sessionKey)
}
