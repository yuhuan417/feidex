package feishuapp

import (
	"context"
	domainsubmission "feidex/internal/domain/submission"

	appdelivery "feidex/internal/adapter/feishu/delivery"
	"feidex/internal/adapter/feishu/quietmode"
	"feidex/internal/adapter/feishu/turnitem"
	apputil "feidex/internal/formatutil"
	"strings"
	"time"
)

func replyInThreadForSubmission(_ *App, _ *domainsubmission.Submission) bool {
	return false
}

func sendSubmissionQueuedNotice(a *App, ctx context.Context, sub *domainsubmission.Submission) {
	if sub == nil {
		return
	}
	sendTurnEventMessages(a, ctx, sub, "已加入队列，等待当前任务结束后开始处理。", replyInThreadForSubmission(a, sub), "turn_queued")
}

func sendSubmissionStartedNotice(a *App, ctx context.Context, sub *domainsubmission.Submission) {
	if sub == nil {
		return
	}
	sendTurnEventMessages(a, ctx, sub, "已轮到这条消息，开始处理。", replyInThreadForSubmission(a, sub), "turn_started")
}

func (s outboundCardService) sendPlanCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, planText, reuseMessageID string) string {
	return newOutboundCardService(s.app).sendTurnEventCardWithReuse(ctx, sub, "计划更新", "blue", "计划:\n"+strings.TrimSpace(planText), "turn_plan", "", reuseMessageID)
}

func (s outboundCardService) sendTurnItemCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, payload turnitem.CardPayload, reuseMessageID string) string {
	if s.app == nil || s.app.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	if strings.TrimSpace(payload.SummaryText) == "" && strings.TrimSpace(payload.DetailText) == "" {
		return ""
	}
	if payload.Title == "" || payload.Color == "" {
		payload.Title, payload.Color = turnitem.TurnItemCardMeta(payload.ItemType, payload.IsFinalAnswer)
	}
	if quietmode.Enabled(s.app.configView().feishuConfig()) && !shouldDeliverTurnItemPayloadInQuiet(quietmode.Mode(s.app.configView().feishuConfig()), payload) {
		return ""
	}
	if payload.ItemType == "user_input" && payload.UserInput != nil {
		return sendAsyncUserInputCard(s.app, sub, *payload.UserInput, reuseMessageID)
	}
	kind := turnitem.TurnItemEventKind(payload.ItemType)
	footerLines := []string(nil)
	if payload.IsFinalAnswer {
		footerLines = s.app.bindings.TurnMetadata.TurnFinalFooterLines(sub.TurnID, time.Now())
	}
	if turnitem.IsReplyTurnItem(payload.ItemType) {
		body := turnitem.ReplyTurnItemCardBody(payload)
		if body == "" {
			body = payload.DetailText
		}
		title := contentCardTitleForSubmission(s.app, sub, turnitem.ReplyTurnItemCardTitle(payload))
		color := payload.Color
		if !payload.IsFinalAnswer {
			title, color, _, _ = outboundMessageCardMeta("turn_output", sub.WorkspaceID)
			title = contentCardTitleForSubmission(s.app, sub, title)
		}
		results := sendReplyCardChunksWithReuse(s.app,
			ctx,
			sub,
			title,
			color,
			appdelivery.BuildReplyCardChunks(body, true, footerLines),
			replyInThreadForSubmission(s.app, sub),
			payload.IsFinalAnswer,
			reuseMessageID,
		)
		if len(results) == 0 {
			fallback := payload.SummaryText
			if fallback == "" {
				fallback = payload.DetailText
			}
			if payload.IsFinalAnswer {
				sendFinalMessagesWithFooter(s.app, ctx, sub, fallback, footerLines, replyInThreadForSubmission(s.app, sub))
			} else {
				sendTurnEventMessages(s.app, ctx, sub, fallback, replyInThreadForSubmission(s.app, sub), kind)
			}
			return ""
		}
		for _, result := range results {
			recordMessageLink(s.app, result.MessageID, kind, sub, payload.ItemID)
			if payload.IsFinalAnswer && result.CardID != "" {
				scheduleLocalFileLinkPatch(s.app, sub, result.CardID, result.Title, color, result.ShowHeader, result.Body, result.FooterLines)
			}
		}
		return results[0].MessageID
	}
	card := newOutboundCardService(s.app).renderTurnItemCard(ctx, sub, payload, payload.IsFinalAnswer)
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := patchCardEffect(ctx, s.app, reuseMessageID, card); err == nil {
			recordMessageLink(s.app, reuseMessageID, kind, sub, payload.ItemID)
			return reuseMessageID
		}
	}
	id, err := replyCardWithIDEffect(ctx, s.app, sub.TriggerMessageID, card, replyInThreadForSubmission(s.app, sub))
	if err != nil || strings.TrimSpace(id) == "" {
		fallback := payload.SummaryText
		if fallback == "" {
			fallback = payload.DetailText
		}
		if payload.IsFinalAnswer {
			sendFinalMessagesWithFooter(s.app, ctx, sub, fallback, footerLines, replyInThreadForSubmission(s.app, sub))
		} else {
			sendTurnEventMessages(s.app, ctx, sub, fallback, replyInThreadForSubmission(s.app, sub), kind)
		}
		return ""
	}
	recordMessageLink(s.app, id, kind, sub, payload.ItemID)
	return id
}

// Exported wrapper for sub-package interface satisfaction.
func (s outboundCardService) ReplaceTurnEventCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, title, color, body, kind, itemID, reuseMessageID string) string {
	return s.replaceTurnEventCardWithReuse(ctx, sub, title, color, body, kind, itemID, reuseMessageID)
}

func (s outboundCardService) replaceTurnEventCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, title, color, body, kind, itemID, reuseMessageID string) string {
	if s.app == nil || s.app.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if strings.TrimSpace(reuseMessageID) != "" {
		card := cardRendererForApp(s.app).renderCompactMarkdownCard(sub, contentCardTitleForSubmission(s.app, sub, title), color, "", body, nil)
		if err := patchCardEffect(ctx, s.app, reuseMessageID, card); err == nil {
			recordMessageLink(s.app, reuseMessageID, kind, sub, itemID)
			return reuseMessageID
		}
	}
	return newOutboundCardService(s.app).sendTurnEventCardWithReuse(ctx, sub, title, color, body, kind, itemID, "")
}

func (s outboundCardService) sendTurnEventCardWithReuse(ctx context.Context, sub *domainsubmission.Submission, title, color, body, kind, itemID, reuseMessageID string) string {
	if s.app == nil || s.app.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	if quietmode.Enabled(s.app.configView().feishuConfig()) && !quietmode.ShouldDeliverTurnKind(quietmode.Mode(s.app.configView().feishuConfig()), kind) {
		return ""
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	card := cardRendererForApp(s.app).renderCompactMarkdownCard(sub, contentCardTitleForSubmission(s.app, sub, title), color, "", body, nil)
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := patchCardEffect(ctx, s.app, reuseMessageID, card); err == nil {
			recordMessageLink(s.app, reuseMessageID, kind, sub, itemID)
			return reuseMessageID
		}
	}
	id, err := replyCardWithIDEffect(ctx, s.app, sub.TriggerMessageID, card, replyInThreadForSubmission(s.app, sub))
	if err != nil || strings.TrimSpace(id) == "" {
		sendTurnEventMessages(s.app, ctx, sub, body, replyInThreadForSubmission(s.app, sub), kind)
		return ""
	}
	recordMessageLink(s.app, id, kind, sub, itemID)
	return id
}

func (s outboundCardService) renderTurnItemCard(ctx context.Context, sub *domainsubmission.Submission, payload turnitem.CardPayload, enablePreview bool) map[string]any {
	if turnitem.IsReplyTurnItem(payload.ItemType) {
		return cardRendererForApp(s.app).renderReplyMarkdownCardWithHeaderOptions(ctx, sub, contentCardTitleForSubmission(s.app, sub, turnitem.ReplyTurnItemCardTitle(payload)), payload.Color, payload.IsFinalAnswer, turnitem.ReplyTurnItemCardBody(payload), nil, enablePreview)
	}
	meta, body := turnitem.CompactTurnItemCardContent(payload)
	return cardRendererForApp(s.app).renderCompactMarkdownCard(sub, contentCardTitleForSubmission(s.app, sub, payload.Title), payload.Color, meta, body, nil)
}

// SendTerminalCard executes the turn use case's semantic terminal effect.
func (s outboundCardService) SendTerminalCard(ctx context.Context, sub *domainsubmission.Submission, text, attentionUserID, reuseMessageID string) {
	s.ReplaceTurnEventCardWithReuse(ctx, sub, "任务状态", "grey", apputil.PrependAttentionMentionMarkdown(text, attentionUserID), "turn_terminal", "", reuseMessageID)
}
