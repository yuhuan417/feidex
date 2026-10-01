package app

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/app/turn"
	"feidex/internal/state"
)

func executeQuietWorkingCardOp(a *App, ctx context.Context, sub *state.Submission, op turn.QuietWorkingCardOp) {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return
	}
	if strings.TrimSpace(op.Body) == "" {
		return
	}
	card := cardRendererForApp(a).renderCompactMarkdownCard(sub, contentCardTitleForSubmission(a, sub, turn.QuietWorkingCardTitle), turn.QuietWorkingCardColor, "", op.Body, nil)
	if strings.TrimSpace(op.MessageID) == "" {
		if strings.TrimSpace(op.Body) == "" {
			return
		}
		messageID, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, replyInThreadForSubmission(a, sub))
		if err != nil || strings.TrimSpace(messageID) == "" {
			slog.Warn("send quiet working card failed",
				"turn_id", op.TurnID,
				"error", err,
			)
			return
		}
		recordMessageLink(a, messageID, "turn_working", sub, "")
		commitQuietWorkingCardRender(a, op.TurnID, messageID, op.Body)
		return
	}
	if err := a.feishu.PatchCard(ctx, op.MessageID, card); err != nil {
		slog.Warn("patch quiet working card failed",
			"turn_id", op.TurnID,
			"message_id", op.MessageID,
			"error", err,
		)
		return
	}
	commitQuietWorkingCardRender(a, op.TurnID, op.MessageID, op.Body)
}

func commitQuietWorkingCardRender(a *App, turnID, messageID, body string) {
	newTurnStreamService(a).commitTurnStreamQuietRender(turnID, messageID, body)
}

// prepareQuietWorkingCardBoundaryLocked wraps turn.PrepareBoundaryLocked for use within the app package.
