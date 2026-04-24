package app

import (
	"context"
	appdelivery "feidex/internal/app/delivery"
	"strings"
	"time"
)

func updateClaudeOutputSegmentWithReuse(a *App, ctx context.Context, threadID, turnID, body, reuseMessageID string) ([]appdelivery.SentReplyChunk, bool) {
	return deliverClaudeOutputSegment(a, ctx, threadID, turnID, body, false, reuseMessageID)
}

func finalizeClaudeOutputSegment(a *App, ctx context.Context, threadID, turnID, body string) bool {
	_, ok := deliverClaudeOutputSegment(a, ctx, threadID, turnID, body, true, "")
	return ok
}

func deliverClaudeOutputSegment(a *App, ctx context.Context, threadID, turnID, body string, final bool, reuseMessageID string) ([]appdelivery.SentReplyChunk, bool) {
	if a == nil {
		return nil, false
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, false
	}
	_, sub := findSubmissionByTurn(a, threadID, turnID)
	if sub == nil {
		return nil, false
	}
	kind := "turn_output"
	if final {
		kind = "final_message"
	}
	if quietModeEnabled(feishuConfig(a)) && !shouldDeliverTurnKindInQuiet(quietMode(feishuConfig(a)), kind) {
		return nil, true
	}

	if final {
		results := sendFinalMessagesWithFooterAndReuse(a, ctx, sub, body, newRuntimeStateService(a).turnFinalFooterLines(turnID, time.Now()), replyInThreadForSubmission(a, sub), nil)
		if len(results) == 0 {
			return nil, false
		}
		newTurnStreamService(a).markTurnStreamFinal(turnID)
		return results, true
	}

	title, color, replyClass, showHeader := outboundMessageCardMeta(kind)
	if !replyClass {
		ids := sendReplyMessagesWithReuse(a, ctx, sub, body, replyInThreadForSubmission(a, sub), kind, reuseMessageID)
		if len(ids) == 0 {
			return nil, false
		}
		return []appdelivery.SentReplyChunk{{MessageID: ids[0], Body: body, Title: title, ShowHeader: showHeader}}, true
	}
	results := sendReplyCardChunksWithReuseIDs(a,
		ctx,
		sub,
		title,
		color,
		appdelivery.BuildReplyCardChunks(body, showHeader, nil),
		replyInThreadForSubmission(a, sub),
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
		recordMessageLink(a, result.MessageID, kind, sub, "")
	}
	return results, true
}
