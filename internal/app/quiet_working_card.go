package app

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/app/turn"
	"feidex/internal/state"
)

func executeQuietWorkingCardOp(a *App, ctx context.Context, sub *state.Submission, op quietWorkingCardOp) {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return
	}
	if strings.TrimSpace(op.Body) == "" {
		return
	}
	card := cardRendererForApp(a).renderCompactMarkdownCard(sub, quietWorkingCardTitle, quietWorkingCardColor, "", op.Body, nil)
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

// toStreamState converts a turnStream to a turn.StreamState for use with turn package functions.
func toStreamState(s *turnStream) *turn.StreamState {
	if s == nil {
		return nil
	}
	return &turn.StreamState{
		TurnID:       s.TurnID,
		QuietWorking: s.QuietWorking,
	}
}

// prepareQuietWorkingCardUpdateLocked wraps turn.PrepareUpdateLocked for use within the app package.
func prepareQuietWorkingCardUpdateLocked(stream *turnStream, itemID string, item map[string]any, workspaceCwd string) quietWorkingCardOp {
	if stream == nil {
		return turn.PrepareUpdateLocked(nil, itemID, item, workspaceCwd)
	}
	ss := toStreamState(stream)
	op := turn.PrepareUpdateLocked(ss, itemID, item, workspaceCwd)
	stream.QuietWorking = ss.QuietWorking
	return op
}

// prepareQuietWorkingCardBoundaryLocked wraps turn.PrepareBoundaryLocked for use within the app package.
func prepareQuietWorkingCardBoundaryLocked(stream *turnStream) quietWorkingBoundary {
	if stream == nil {
		return turn.PrepareBoundaryLocked(nil)
	}
	ss := toStreamState(stream)
	boundary := turn.PrepareBoundaryLocked(ss)
	stream.QuietWorking = ss.QuietWorking
	return boundary
}
