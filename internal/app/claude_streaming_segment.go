package app

import (
	"context"
	"strings"
	"time"
)

func (a *App) updateClaudeOutputSegmentWithReuse(ctx context.Context, threadID, turnID, body, reuseMessageID string) ([]sentReplyChunk, bool) {
	return a.deliverClaudeOutputSegment(ctx, threadID, turnID, body, false, reuseMessageID)
}

func (a *App) finalizeClaudeOutputSegment(ctx context.Context, threadID, turnID, body string) bool {
	_, ok := a.deliverClaudeOutputSegment(ctx, threadID, turnID, body, true, "")
	return ok
}

func (a *App) deliverClaudeOutputSegment(ctx context.Context, threadID, turnID, body string, final bool, reuseMessageID string) ([]sentReplyChunk, bool) {
	if a == nil {
		return nil, false
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, false
	}
	_, sub := a.findSubmissionByTurn(threadID, turnID)
	if sub == nil {
		return nil, false
	}
	kind := "turn_output"
	if final {
		kind = "final_message"
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet(a.quietMode(), kind) {
		return nil, true
	}

	if final {
		results := a.sendFinalMessagesWithFooterAndReuse(ctx, sub, body, a.turnFinalFooterLines(turnID, time.Now()), a.replyInThreadForSubmission(sub), nil)
		if len(results) == 0 {
			return nil, false
		}
		a.markTurnStreamFinal(turnID)
		return results, true
	}

	title, color, replyClass, showHeader := outboundMessageCardMeta(kind)
	if !replyClass {
		ids := a.sendReplyMessagesWithReuse(ctx, sub, body, a.replyInThreadForSubmission(sub), kind, reuseMessageID)
		if len(ids) == 0 {
			return nil, false
		}
		return []sentReplyChunk{{MessageID: ids[0], Body: body, Title: title, ShowHeader: showHeader}}, true
	}
	results := a.sendReplyCardChunksWithReuseIDs(
		ctx,
		sub,
		title,
		color,
		buildReplyCardChunks(body, showHeader, nil),
		a.replyInThreadForSubmission(sub),
		false,
		func() []string {
			if strings.TrimSpace(reuseMessageID) == "" {
				return nil
			}
			return []string{strings.TrimSpace(reuseMessageID)}
		}(),
	)
	if len(results) == 0 {
		return nil, false
	}
	for _, result := range results {
		a.recordMessageLink(result.MessageID, kind, sub, "")
	}
	return results, true
}
