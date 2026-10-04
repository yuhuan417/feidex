package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/turn"
	turnstream "feidex/internal/adapter/feishu/turnstream"
	domainsubmission "feidex/internal/domain/submission"
	"log/slog"
	"strings"
)

func executeQuietWorkingCardOp(a *App, ctx context.Context, sub *domainsubmission.Submission, op turn.QuietWorkingCardOp) {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return
	}
	if strings.TrimSpace(op.Body) == "" {
		return
	}
	card := newCardRenderer(a.Config()).renderCompactMarkdownCard(sub, contentCardTitleForSubmission(a.State(), sub, turn.QuietWorkingCardTitle), turn.QuietWorkingCardColor, "", op.Body, nil)
	if strings.TrimSpace(op.MessageID) == "" {
		if strings.TrimSpace(op.Body) == "" {
			return
		}
		messageID, err := replyCardWithIDEffect(ctx, a, sub.TriggerMessageID, card, replyInThreadForSubmission(sub))
		if err != nil || strings.TrimSpace(messageID) == "" {
			slog.Warn("send quiet working card failed",
				"turn_id", op.TurnID,
				"error", err,
			)
			return
		}
		recordMessageLink(a, messageID, "turn_working", sub, "")
		commitQuietWorkingCardRender(a.bindings.TurnPresentation, op.TurnID, messageID, op.Body)
		return
	}
	if err := patchCardEffect(ctx, a, op.MessageID, card); err != nil {
		slog.Warn("patch quiet working card failed",
			"turn_id", op.TurnID,
			"message_id", op.MessageID,
			"error", err,
		)
		return
	}
	commitQuietWorkingCardRender(a.bindings.TurnPresentation, op.TurnID, op.MessageID, op.Body)
}

func commitQuietWorkingCardRender(turnpresentation *turnstream.Service, turnID, messageID, body string) {
	turnpresentation.CommitStreamQuietRender(turnID, messageID, body)
}

// prepareQuietWorkingCardBoundaryLocked wraps turn.PrepareBoundaryLocked for use within the app package.
