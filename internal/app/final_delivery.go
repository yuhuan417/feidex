package app

import (
	"context"
	"strings"

	"feidex/internal/state"
)

func (a *App) sendFinalMessages(ctx context.Context, sub *state.Submission, text string, inThread bool) []string {
	return a.sendFinalMessagesWithFooter(ctx, sub, text, nil, inThread)
}

func (a *App) sendEmptyFinalCard(ctx context.Context, sub *state.Submission, footerLines []string) string {
	return a.sendEmptyFinalCardWithReuse(ctx, sub, footerLines, "")
}

func (a *App) sendEmptyFinalCardWithReuse(ctx context.Context, sub *state.Submission, footerLines []string, reuseMessageID string) string {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return ""
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet(a.quietMode(), "final_message") {
		return ""
	}
	card := a.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, "最终答复", "green", true, "", nil, true)
	appendReplyCardFooter(card, footerLines)
	if strings.TrimSpace(reuseMessageID) != "" {
		if err := a.feishu.PatchCard(ctx, reuseMessageID, card); err == nil {
			a.recordMessageLink(reuseMessageID, "final_message", sub, "")
			return reuseMessageID
		}
	}
	id, err := a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, a.replyInThreadForSubmission(sub))
	if err != nil || strings.TrimSpace(id) == "" {
		return ""
	}
	a.recordMessageLink(id, "final_message", sub, "")
	return id
}

func (a *App) sendFinalMessagesWithFooter(ctx context.Context, sub *state.Submission, text string, footerLines []string, inThread bool) []string {
	results := a.sendFinalMessagesWithFooterAndReuse(ctx, sub, text, footerLines, inThread, nil)
	if len(results) == 0 {
		return nil
	}
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.MessageID)
	}
	return ids
}

func (a *App) sendFinalMessagesWithFooterAndReuse(ctx context.Context, sub *state.Submission, text string, footerLines []string, inThread bool, reuseMessageIDs []string) []sentReplyChunk {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return nil
	}
	if a.quietModeEnabled() && !shouldDeliverTurnKindInQuiet(a.quietMode(), "final_message") {
		return nil
	}
	chunks := buildReplyCardChunks(strings.TrimSpace(text), true, footerLines)
	results := a.sendReplyCardChunksWithReuseIDs(ctx, sub, "最终答复", "green", chunks, inThread, true, reuseMessageIDs)
	if len(results) == 0 {
		return nil
	}
	for _, result := range results {
		a.recordMessageLink(result.MessageID, "final_message", sub, "")
		if result.CardID != "" {
			a.scheduleLocalFileLinkPatch(sub, result.CardID, result.Title, "green", result.ShowHeader, result.Body, result.FooterLines)
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
	appendMarkdownBodyCardElement(card, map[string]any{
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
