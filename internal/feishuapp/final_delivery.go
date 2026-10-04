package feishuapp

import (
	"context"
	domainsubmission "feidex/internal/domain/submission"
	apputil "feidex/internal/formatutil"

	appdelivery "feidex/internal/adapter/feishu/delivery"
	"feidex/internal/adapter/feishu/quietmode"
	"strings"

	appcards "feidex/internal/adapter/feishu/cards"
)

func (d replyChunkDelivery) SendEmptyFinalCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, footerLines []string, reuseMessageID string) string {
	if !d.ready || sub == nil {
		return ""
	}
	feishuConfig := d.config.feishuConfig()
	if quietmode.Enabled(feishuConfig) && !quietmode.ShouldDeliverTurnKind(quietmode.Mode(feishuConfig), "final_message") {
		return ""
	}
	triggerMessageID := strings.TrimSpace(sub.TriggerMessageID)
	inThread := replyInThreadForSubmission(sub)
	attentionUserID := turnStopAttentionUserID(d.state, sub, sub.TurnID)
	fallbackText := appendFooterText(apputil.PrependAttentionMentionMarkdown("任务已结束。", attentionUserID), footerLines)
	body := apputil.PrependAttentionMentionMarkdown("", attentionUserID)
	title, color, _, showHeader := outboundMessageCardMeta("final_message", sub.WorkspaceID)
	card := d.renderer.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, contentCardTitleForSubmission(d.state, sub, title), color, showHeader, body, nil, true)
	appendReplyCardFooter(card, footerLines)
	if reuseMessageID = strings.TrimSpace(reuseMessageID); reuseMessageID != "" {
		if err := d.outbound.PatchCard(ctx, reuseMessageID, card); err == nil {
			d.links.Record(reuseMessageID, "final_message", anchorForSubmission(sub), "")
			return reuseMessageID
		}
	}
	if triggerMessageID != "" {
		id, err := d.outbound.ReplyCard(ctx, triggerMessageID, card, inThread)
		if err == nil && strings.TrimSpace(id) != "" {
			d.links.Record(id, "final_message", anchorForSubmission(sub), "")
			return id
		}
		id, err = d.outbound.ReplyTextWithID(ctx, triggerMessageID, fallbackText, inThread)
		if err == nil && strings.TrimSpace(id) != "" {
			d.links.Record(id, "final_message", anchorForSubmission(sub), "")
			return id
		}
	}
	if chatID := strings.TrimSpace(sub.ChatID); chatID != "" {
		_ = d.outbound.SendText(ctx, chatID, fallbackText)
	}
	return ""
}

func sendFinalMessagesWithFooterAndReuse(delivery replyChunkDelivery, ctx context.Context, sub *domainsubmission.Submission, text string, footerLines []string, inThread bool, reuseMessageIDs []string) []appdelivery.SentReplyChunk {
	if !delivery.ready || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	feishuConfig := delivery.config.feishuConfig()
	if quietmode.Enabled(feishuConfig) && !quietmode.ShouldDeliverTurnKind(quietmode.Mode(feishuConfig), "final_message") {
		return nil
	}
	title, color, _, _ := outboundMessageCardMeta("final_message", sub.WorkspaceID)
	chunks := appdelivery.BuildReplyCardChunks(strings.TrimSpace(text), true, footerLines)
	results := delivery.SendWithReuseIDs(ctx, sub, title, color, chunks, inThread, true, reuseMessageIDs)
	if len(results) == 0 {
		return nil
	}
	return results
}

func appendReplyCardFooter(card map[string]any, footerLines []string) {
	lines := make([]string, 0, len(footerLines))
	for _, line := range footerLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return
	}
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":        "plain_text",
			"content":    strings.Join(lines, "\n"),
			"text_size":  "notation",
			"text_color": "grey",
		},
	})
}

func appendFooterText(body string, footerLines []string) string {
	parts := []string{}
	if strings.TrimSpace(body) != "" {
		parts = append(parts, strings.TrimSpace(body))
	}
	for _, line := range footerLines {
		line = strings.TrimSpace(line)
		if line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, "\n")
}
