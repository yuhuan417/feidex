package app

import (
	"context"
	"strings"
	"time"
)

func (a *App) updateClaudeOutputSegment(ctx context.Context, threadID, turnID, body string) bool {
	return a.deliverClaudeOutputSegment(ctx, threadID, turnID, body, false, "")
}

func (a *App) updateClaudeOutputSegmentWithReuse(ctx context.Context, threadID, turnID, body, reuseMessageID string) bool {
	return a.deliverClaudeOutputSegment(ctx, threadID, turnID, body, false, reuseMessageID)
}

func (a *App) finalizeClaudeOutputSegment(ctx context.Context, threadID, turnID, body string) bool {
	return a.deliverClaudeOutputSegment(ctx, threadID, turnID, body, true, "")
}

func (a *App) closeClaudeOutputSegment(threadID, turnID string) {
}

func (a *App) deliverClaudeOutputSegment(ctx context.Context, threadID, turnID, body string, final bool, reuseMessageID string) bool {
	if a == nil {
		return false
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	_, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return false
	}
	kind := "turn_output"
	if final {
		kind = "final_message"
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet(a.quietMode(), kind) {
		return true
	}

	if final {
		ids := a.sendFinalMessagesWithFooter(ctx, sub, body, a.turnFinalFooterLines(turnID, time.Now()), a.replyInThreadForSubmission(sub))
		if len(ids) == 0 {
			return false
		}
		a.turnStreamsMu.Lock()
		if stream := a.turnStreams[turnID]; stream != nil {
			stream.SentFinal = true
		}
		a.turnStreamsMu.Unlock()
		return true
	}

	return len(a.sendReplyMessagesWithReuse(ctx, sub, body, a.replyInThreadForSubmission(sub), kind, reuseMessageID)) > 0
}
