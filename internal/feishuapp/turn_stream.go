package feishuapp

import (
	"context"
	appturnstream "feidex/internal/adapter/feishu/turnstream"
	domainsubmission "feidex/internal/domain/submission"
	"strings"
)

type turnStream = appturnstream.Stream
type turnStreamFlushResult = appturnstream.FlushResult

func maybeSendSubmissionStartedNotice(a *App, ctx context.Context, sub *domainsubmission.Submission) {
	if a == nil || sub == nil || strings.TrimSpace(sub.ID) == "" {
		return
	}
	appState := a.State()
	shouldSend, err := a.bindings.SubmissionStatus.ClaimStartNotice(sub.ID)
	if err != nil || !shouldSend {
		return
	}
	updated := appState.Submission(sub.ID)
	if updated != nil {
		sub = updated
	}
	sendSubmissionStartedNotice(a, ctx, sub)
}
