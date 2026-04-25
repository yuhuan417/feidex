package app

import (
	"context"

	appreview "feidex/internal/app/review"
	appreviewcmd "feidex/internal/app/reviewcmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// Type and constant aliases — reviewcmd exported types
// ---------------------------------------------------------------------------

type reviewPendingPayload = appreviewcmd.ReviewPendingPayload

const (
	submissionKindReview = appreviewcmd.SubmissionKindReview
	pendingKindReview    = appreviewcmd.PendingKindReview

	reviewFormModeBase   = appreviewcmd.ReviewFormModeBase
	reviewFormModeCommit = appreviewcmd.ReviewFormModeCommit
	reviewFormModeCustom = appreviewcmd.ReviewFormModeCustom
)

// ---------------------------------------------------------------------------
// Function wrappers — delegate to reviewcmd
// ---------------------------------------------------------------------------

func isReviewSubmission(sub *state.Submission) bool {
	return appreviewcmd.IsReviewSubmission(sub)
}

func reviewPendingPayloadFromPending(pending *state.PendingRequest) reviewPendingPayload {
	return appreviewcmd.ReviewPendingPayloadFromPending(pending)
}

func commandReview(a *App, msg *feishu.InboundMessage, args []string) error {
	return appreviewcmd.CommandReview(a, msg, args)
}

func startInlineReview(a *App, msg *feishu.InboundMessage, target appreview.TargetSpec) (string, error) {
	return appreviewcmd.StartInlineReview(a, msg, target)
}

func enqueueReviewSubmission(a *App, msg *feishu.InboundMessage, sessionKey string, ws *config.Workspace, threadID string, target appreview.TargetSpec) error {
	return appreviewcmd.EnqueueReviewSubmission(a, msg, sessionKey, ws, threadID, target)
}

func startSubmissionReview(a *App, ctx context.Context, threadID string, sub *state.Submission) (string, error) {
	return appreviewcmd.StartSubmissionReview(a, ctx, threadID, sub)
}

func reviewTargetFromSubmission(sub *state.Submission) appreview.TargetSpec {
	return appreviewcmd.ReviewTargetFromSubmission(sub)
}

func reviewTargetParams(target appreview.TargetSpec) map[string]any {
	return appreviewcmd.ReviewTargetParams(target)
}
