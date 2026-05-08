package app

import (
	"context"
	appdelivery "feidex/internal/app/delivery"
	"strings"

	appcards "feidex/internal/app/cards"
	"feidex/internal/state"
)

func sendFinalMessages(a *App, ctx context.Context, sub *state.Submission, text string, inThread bool) []string {
	return sendFinalMessagesWithFooter(a, ctx, sub, text, nil, inThread)
}

func sendEmptyFinalCard(a *App, ctx context.Context, sub *state.Submission, footerLines []string) string {
	return sendEmptyFinalCardWithReuse(a, ctx, sub, footerLines, "")
}

func sendEmptyFinalCardWithReuse(a *App, ctx context.Context, sub *state.Submission, footerLines []string, reuseMessageID string) string {
	if a == nil || a.feishu == nil || sub == nil {
		return ""
	}
	if quietModeEnabled(feishuConfig(a)) && !shouldDeliverTurnKindInQuiet(quietMode(feishuConfig(a)), "final_message") {
		return ""
	}
	triggerMessageID := strings.TrimSpace(sub.TriggerMessageID)
	inThread := replyInThreadForSubmission(a, sub)
	fallbackText := appendFooterText(prependAttentionMentionMarkdown("任务已结束。", turnStopAttentionUserID(a, sub, sub.TurnID)), footerLines)
	body := prependAttentionMentionMarkdown("", turnStopAttentionUserID(a, sub, sub.TurnID))
	title, color, _, showHeader := outboundMessageCardMeta("final_message", sub.WorkspaceID)
	card := cardRendererForApp(a).renderReplyMarkdownCardWithHeaderOptions(ctx, sub, title, color, showHeader, body, nil, true)
	appendReplyCardFooter(card, footerLines)
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := a.feishu.PatchCard(ctx, reuseMessageID, card); err == nil {
			recordMessageLink(a, reuseMessageID, "final_message", sub, "")
			return reuseMessageID
		}
	}
	if triggerMessageID != "" {
		id, err := a.feishu.ReplyCard(ctx, triggerMessageID, card, inThread)
		if err == nil && strings.TrimSpace(id) != "" {
			recordMessageLink(a, id, "final_message", sub, "")
			return id
		}
		id, err = a.feishu.ReplyTextWithID(ctx, triggerMessageID, fallbackText, inThread)
		if err == nil && strings.TrimSpace(id) != "" {
			recordMessageLink(a, id, "final_message", sub, "")
			return id
		}
	}
	if chatID := strings.TrimSpace(sub.ChatID); chatID != "" {
		if err := a.feishu.SendText(ctx, chatID, fallbackText); err == nil {
			return ""
		}
	}
	return ""
}

func sendFinalMessagesWithFooter(a *App, ctx context.Context, sub *state.Submission, text string, footerLines []string, inThread bool) []string {
	results := sendFinalMessagesWithFooterAndReuse(a, ctx, sub, text, footerLines, inThread, nil)
	if len(results) == 0 {
		return nil
	}
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.MessageID)
	}
	return ids
}

func sendFinalMessagesWithFooterAndReuse(a *App, ctx context.Context, sub *state.Submission, text string, footerLines []string, inThread bool, reuseMessageIDs []string) []appdelivery.SentReplyChunk {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	if quietModeEnabled(feishuConfig(a)) && !shouldDeliverTurnKindInQuiet(quietMode(feishuConfig(a)), "final_message") {
		return nil
	}
	title, color, _, _ := outboundMessageCardMeta("final_message", sub.WorkspaceID)
	chunks := appdelivery.BuildReplyCardChunks(strings.TrimSpace(text), true, footerLines)
	results := sendReplyCardChunksWithReuseIDs(a, ctx, sub, title, color, chunks, inThread, true, reuseMessageIDs)
	if len(results) == 0 {
		return nil
	}
	for _, result := range results {
		recordMessageLink(a, result.MessageID, "final_message", sub, "")
		if result.CardID != "" {
			newFinalCardPatchService(a).registerFinalCardPatchState(result.CardID, sub, result.Title, "green", result.ShowHeader, result.Body, result.FooterLines)
			scheduleLocalFileLinkPatch(a, sub, result.CardID, result.Title, "green", result.ShowHeader, result.Body, result.FooterLines)
		}
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
