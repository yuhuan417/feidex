package app

import (
	"context"
	domainsubmission "feidex/internal/domain/submission"

	"feidex/internal/feishu"
)

func (w *submissionCoordinator) enqueueSubmissionWithSessionKey(msg *feishu.InboundMessage, sessionKey string, bindOnlyCurrentRoot bool) error {
	return newSubmissionQueueServiceFromApp(w.app).EnqueueSubmission(msg, sessionKey, bindOnlyCurrentRoot)
}

func (w *submissionCoordinator) notifySubmissionStartFailure(ctx context.Context, sub *domainsubmission.Submission, err error, willContinue bool) {
	newSubmissionQueueServiceFromApp(w.app).NotifySubmissionStartFailure(ctx, sub, err, willContinue)
}
