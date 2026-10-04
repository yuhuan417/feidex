package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/planmode"
	"feidex/internal/adapter/feishu/turn"
	turnstream "feidex/internal/adapter/feishu/turnstream"
	domainsubmission "feidex/internal/domain/submission"
	"log/slog"
	"strings"
)

type quietWorkingCardExecutor struct {
	renderer cardRenderer
	state    planmode.SessionStateProvider
	outbound effectOutbound
	links    messageLinkRecorder
	turns    *turnstream.Service
	ready    bool
}

func (e quietWorkingCardExecutor) ExecuteQuietWorkingCardOp(ctx context.Context, sub *domainsubmission.Submission, op turn.QuietWorkingCardOp) {
	if !e.ready || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return
	}
	if strings.TrimSpace(op.Body) == "" {
		return
	}
	card := e.renderer.renderCompactMarkdownCard(sub, contentCardTitleForSubmission(e.state, sub, turn.QuietWorkingCardTitle), turn.QuietWorkingCardColor, "", op.Body, nil)
	if strings.TrimSpace(op.MessageID) == "" {
		messageID, err := e.outbound.ReplyCard(ctx, sub.TriggerMessageID, card, replyInThreadForSubmission(sub))
		if err != nil || strings.TrimSpace(messageID) == "" {
			slog.Warn("send quiet working card failed", "turn_id", op.TurnID, "error", err)
			return
		}
		e.links.Record(messageID, "turn_working", anchorForSubmission(sub), "")
		commitQuietWorkingCardRender(e.turns, op.TurnID, messageID, op.Body)
		return
	}
	if err := e.outbound.PatchCard(ctx, op.MessageID, card); err != nil {
		slog.Warn("patch quiet working card failed", "turn_id", op.TurnID, "message_id", op.MessageID, "error", err)
		return
	}
	commitQuietWorkingCardRender(e.turns, op.TurnID, op.MessageID, op.Body)
}

func commitQuietWorkingCardRender(turnpresentation *turnstream.Service, turnID, messageID, body string) {
	turnpresentation.CommitStreamQuietRender(turnID, messageID, body)
}

// prepareQuietWorkingCardBoundaryLocked wraps turn.PrepareBoundaryLocked for use within the app package.
