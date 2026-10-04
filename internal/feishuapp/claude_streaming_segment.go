package feishuapp

import (
	"context"
	"strings"
	"time"

	appdelivery "feidex/internal/adapter/feishu/delivery"
	"feidex/internal/adapter/feishu/quietmode"
	domainsubmission "feidex/internal/domain/submission"
)

type claudeOutputSegmentDelivery struct {
	delivery        replyChunkDelivery
	findSubmission  func(string, string) (string, *domainsubmission.Submission)
	markStreamFinal func(string)
	turnFinalFooter func(string, time.Time) []string
}

func (d claudeOutputSegmentDelivery) Update(ctx context.Context, threadID, turnID, body, reuseMessageID string) ([]appdelivery.SentReplyChunk, bool) {
	return d.deliver(ctx, threadID, turnID, body, false, reuseMessageID)
}

func (d claudeOutputSegmentDelivery) Finalize(ctx context.Context, threadID, turnID, body string) bool {
	_, ok := d.deliver(ctx, threadID, turnID, body, true, "")
	return ok
}

func (d claudeOutputSegmentDelivery) deliver(ctx context.Context, threadID, turnID, body string, final bool, reuseMessageID string) ([]appdelivery.SentReplyChunk, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, false
	}
	_, sub := d.findSubmission(threadID, turnID)
	if sub == nil {
		return nil, false
	}
	kind := "turn_output"
	if final {
		kind = "final_message"
	}
	feishuConfig := d.delivery.config.feishuConfig()
	if quietmode.Enabled(feishuConfig) && !quietmode.ShouldDeliverTurnKind(quietmode.Mode(feishuConfig), kind) {
		return nil, true
	}
	inThread := replyInThreadForSubmission(sub)
	if final {
		title, color, _, _ := outboundMessageCardMeta(kind, sub.WorkspaceID)
		results := d.delivery.SendWithReuseIDs(ctx, sub, title, color, appdelivery.BuildReplyCardChunks(body, true, d.turnFinalFooter(turnID, time.Now())), inThread, true, nil)
		if len(results) == 0 {
			return nil, false
		}
		d.markStreamFinal(turnID)
		return results, true
	}

	title, color, replyClass, showHeader := outboundMessageCardMeta(kind, sub.WorkspaceID)
	if !replyClass {
		ids := d.delivery.SendMessagesWithReuse(ctx, sub, body, inThread, kind, reuseMessageID)
		if len(ids) == 0 {
			return nil, false
		}
		return []appdelivery.SentReplyChunk{{MessageID: ids[0], Body: body, Title: title, ShowHeader: showHeader}}, true
	}
	results := d.delivery.SendWithReuse(ctx, sub, title, color, appdelivery.BuildReplyCardChunks(body, showHeader, nil), inThread, false, reuseMessageID)
	if len(results) == 0 {
		return nil, false
	}
	for _, result := range results {
		d.delivery.links.Record(result.MessageID, kind, anchorForSubmission(sub), "")
	}
	return results, true
}
