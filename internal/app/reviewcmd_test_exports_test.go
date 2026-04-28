package app

import (
	"context"

	appreview "feidex/internal/app/review"
	appreviewcmd "feidex/internal/app/reviewcmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func commandReview(a *App, msg *feishu.InboundMessage, args []string) error {
	return appreviewcmd.CommandReview(newReviewAppAdapter(a), msg, args)
}

func startInlineReview(a *App, msg *feishu.InboundMessage, target appreview.TargetSpec) (string, error) {
	return appreviewcmd.StartInlineReview(newReviewAppAdapter(a), msg, target)
}

func enqueueReviewSubmission(a *App, msg *feishu.InboundMessage, sessionKey string, ws *config.Workspace, threadID string, target appreview.TargetSpec) error {
	return appreviewcmd.EnqueueReviewSubmission(newReviewAppAdapter(a), msg, sessionKey, ws, threadID, target)
}

func startSubmissionReview(a *App, ctx context.Context, threadID string, sub *state.Submission) (string, error) {
	return appreviewcmd.StartSubmissionReview(newReviewAppAdapter(a), ctx, threadID, sub)
}

func reviewTargetFromSubmission(sub *state.Submission) appreview.TargetSpec {
	return appreviewcmd.ReviewTargetFromSubmission(sub)
}

func reviewTargetParams(target appreview.TargetSpec) map[string]any {
	return appreviewcmd.ReviewTargetParams(target)
}

type reviewFormService struct {
	inner appreviewcmd.ReviewFormService
}

func newReviewFormService(app *App) reviewFormService {
	return reviewFormService{inner: newReviewFormServiceInner(app)}
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

type reviewGitService struct{}

func newReviewGitService(_ *App) reviewGitService {
	return reviewGitService{}
}

func (s reviewGitService) resolveReviewTarget(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error) {
	return appreview.NewGitService().ResolveTarget(cwd, target)
}

func (s reviewGitService) listReviewBranches(cwd string) ([]appreview.BranchOption, error) {
	return appreview.NewGitService().ListBranches(cwd)
}

func (s reviewGitService) listReviewCommits(cwd string, limit int) ([]appreview.CommitOption, error) {
	return appreview.NewGitService().ListCommits(cwd, limit)
}

func completeMenuReviewUncommitted(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return appreviewcmd.CompleteMenuReviewUncommitted(newReviewAppAdapter(a), action, sessionKey)
}

func completeMenuReviewBase(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return appreviewcmd.CompleteMenuReviewBase(newReviewAppAdapter(a), action, sessionKey)
}

func completeMenuReviewCommit(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return appreviewcmd.CompleteMenuReviewCommit(newReviewAppAdapter(a), action, sessionKey)
}
