package feishuapp

import (
	"context"
	"strings"

	appturnstream "feidex/internal/adapter/feishu/turnstream"
	appstate "feidex/internal/adapter/storage/json/scoped"
	appsubmission "feidex/internal/application/submission"
	domainsubmission "feidex/internal/domain/submission"
)

type turnStream = appturnstream.Stream
type turnStreamFlushResult = appturnstream.FlushResult

func maybeSendSubmissionStartedNotice(status appsubmission.StatusService, state *appstate.Store, cards OutboundCardService, ctx context.Context, sub *domainsubmission.Submission) {
	if sub == nil || strings.TrimSpace(sub.ID) == "" {
		return
	}
	shouldSend, err := status.ClaimStartNotice(sub.ID)
	if err != nil || !shouldSend {
		return
	}
	updated := state.Submission(sub.ID)
	if updated != nil {
		sub = updated
	}
	cards.replyChunks.SendMessagesWithReuse(ctx, sub, "已轮到这条消息，开始处理。", replyInThreadForSubmission(sub), "turn_started", "")
}
