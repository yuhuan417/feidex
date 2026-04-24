package app

import (
	"context"
	appdelivery "feidex/internal/app/delivery"
	"fmt"
	"strings"

	"feidex/internal/state"
)

type replyChunkRenderSpec struct {
	Title         string
	Color         string
	Body          string
	FooterLines   []string
	ShowHeader    bool
	EnablePreview bool
}

func (a *App) prepareReplyChunkRenderSpecs(ctx context.Context, sub *state.Submission, title, color string, chunks []appdelivery.ReplyCardChunk, enablePreview bool) []replyChunkRenderSpec {
	if a == nil {
		return nil
	}
	if strings.TrimSpace(title) == "最终答复" && len(chunks) > 0 {
		copied := append([]appdelivery.ReplyCardChunk(nil), chunks...)
		copied[0].Body = prependAttentionMentionMarkdown(copied[0].Body, a.turnStopAttentionUserID(sub, sub.TurnID))
		chunks = copied
	}
	chunks = a.fitReplyCardChunks(ctx, sub, title, color, chunks, enablePreview)
	if len(chunks) == 0 {
		return nil
	}
	specs := make([]replyChunkRenderSpec, 0, len(chunks))
	for i, chunk := range chunks {
		effectiveTitle := title
		showHeader := chunk.ShowHeader
		if strings.TrimSpace(title) == "最终答复" && len(chunks) > 1 {
			effectiveTitle = fmt.Sprintf("%s %d/%d", strings.TrimSpace(title), i+1, len(chunks))
			showHeader = true
		}
		specs = append(specs, replyChunkRenderSpec{
			Title:         effectiveTitle,
			Color:         color,
			Body:          chunk.Body,
			FooterLines:   append([]string(nil), chunk.FooterLines...),
			ShowHeader:    showHeader,
			EnablePreview: enablePreview,
		})
	}
	return specs
}

func (a *App) sendReplyChunk(ctx context.Context, sub *state.Submission, spec replyChunkRenderSpec, inThread bool, reuseMessageID string) (appdelivery.SentReplyChunk, bool) {
	if a == nil || a.feishu == nil || sub == nil || strings.TrimSpace(sub.TriggerMessageID) == "" {
		return appdelivery.SentReplyChunk{}, false
	}

	card := a.cardRenderer().renderReplyMarkdownCardWithHeaderOptions(ctx, sub, spec.Title, spec.Color, spec.ShowHeader, spec.Body, nil, spec.EnablePreview)
	appendReplyCardFooter(card, spec.FooterLines)

	cardID := ""
	id := ""
	var err error
	if strings.TrimSpace(reuseMessageID) != "" {
		id = strings.TrimSpace(reuseMessageID)
		err = a.feishu.PatchCard(ctx, id, card)
		if err == nil {
			cardID = id
		} else {
			id, err = a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, inThread)
			if err == nil && strings.TrimSpace(id) != "" {
				cardID = strings.TrimSpace(id)
			}
		}
	} else {
		id, err = a.feishu.ReplyCard(ctx, sub.TriggerMessageID, card, inThread)
		if err == nil && strings.TrimSpace(id) != "" {
			cardID = strings.TrimSpace(id)
		}
	}
	if err != nil || strings.TrimSpace(id) == "" {
		fallback := appendFooterText(strings.TrimSpace(spec.Body), spec.FooterLines)
		id, err = a.feishu.ReplyTextWithID(ctx, sub.TriggerMessageID, fallback, inThread)
	}
	if err != nil || strings.TrimSpace(id) == "" {
		return appdelivery.SentReplyChunk{}, false
	}
	return appdelivery.SentReplyChunk{
		MessageID:   strings.TrimSpace(id),
		CardID:      cardID,
		Title:       spec.Title,
		Body:        spec.Body,
		FooterLines: append([]string(nil), spec.FooterLines...),
		ShowHeader:  spec.ShowHeader,
	}, true
}
