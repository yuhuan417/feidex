package feishuapp

import (
	"context"
	"encoding/json"
	domainsubmission "feidex/internal/domain/submission"
	"strings"

	appdelivery "feidex/internal/adapter/feishu/delivery"
)

func fitReplyCardChunks(renderer cardRenderer, ctx context.Context, sub *domainsubmission.Submission, title, color string, chunks []appdelivery.ReplyCardChunk, enablePreview bool) []appdelivery.ReplyCardChunk {
	if len(chunks) == 0 {
		return nil
	}
	fitted := make([]appdelivery.ReplyCardChunk, 0, len(chunks))
	for _, chunk := range chunks {
		fitted = append(fitted, expandReplyCardChunkToFit(renderer, ctx, sub, title, color, chunk, enablePreview)...)
	}
	return fitted
}

func expandReplyCardChunkToFit(renderer cardRenderer, ctx context.Context, sub *domainsubmission.Submission, title, color string, chunk appdelivery.ReplyCardChunk, enablePreview bool) []appdelivery.ReplyCardChunk {
	if replyCardChunkFits(renderer, ctx, sub, title, color, chunk, enablePreview) {
		return []appdelivery.ReplyCardChunk{chunk}
	}

	blocks := appdelivery.SplitMarkdownBlocks(chunk.Body)
	if len(blocks) == 0 {
		return []appdelivery.ReplyCardChunk{chunk}
	}

	result := make([]appdelivery.ReplyCardChunk, 0, len(blocks))
	current := appdelivery.ReplyCardChunk{ShowHeader: chunk.ShowHeader}
	for len(blocks) > 0 {
		block := blocks[0]
		blocks = blocks[1:]
		candidate := current
		candidate.Body = appdelivery.JoinReplyChunkBodies(current.Body, block.Text)
		if replyCardChunkFits(renderer, ctx, sub, title, color, candidate, enablePreview) {
			current = candidate
			continue
		}
		if strings.TrimSpace(current.Body) != "" {
			result = append(result, current)
			current = appdelivery.ReplyCardChunk{ShowHeader: false}
			blocks = append([]appdelivery.MarkdownSplitBlock{block}, blocks...)
			continue
		}
		if block.TableCount > 0 {
			current.Body = strings.TrimSpace(block.Text)
			result = append(result, current)
			current = appdelivery.ReplyCardChunk{ShowHeader: false}
			continue
		}
		parts := appdelivery.SplitReplyTextBlockToFit(block.Text, func(part string) bool {
			return replyCardChunkFits(renderer, ctx, sub, title, color, appdelivery.ReplyCardChunk{
				Body:       part,
				ShowHeader: current.ShowHeader,
			}, enablePreview)
		})
		if len(parts) <= 1 {
			current.Body = strings.TrimSpace(block.Text)
			result = append(result, current)
			current = appdelivery.ReplyCardChunk{ShowHeader: false}
			continue
		}
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			result = append(result, appdelivery.ReplyCardChunk{
				Body:       part,
				ShowHeader: current.ShowHeader,
			})
			current = appdelivery.ReplyCardChunk{ShowHeader: false}
		}
	}
	if strings.TrimSpace(current.Body) != "" || len(result) == 0 {
		result = append(result, current)
	}

	last := len(result) - 1
	result[last].FooterLines = append([]string(nil), chunk.FooterLines...)
	if replyCardChunkFits(renderer, ctx, sub, title, color, result[last], enablePreview) {
		return result
	}

	footerOnly := appdelivery.ReplyCardChunk{
		ShowHeader:  false,
		FooterLines: append([]string(nil), chunk.FooterLines...),
	}
	result[last].FooterLines = nil
	if replyCardChunkFits(renderer, ctx, sub, title, color, result[last], enablePreview) && replyCardChunkFits(renderer, ctx, sub, title, color, footerOnly, enablePreview) {
		return append(result, footerOnly)
	}
	result[last].FooterLines = append([]string(nil), chunk.FooterLines...)
	return result
}

func replyCardChunkFits(renderer cardRenderer, ctx context.Context, sub *domainsubmission.Submission, title, color string, chunk appdelivery.ReplyCardChunk, enablePreview bool) bool {
	card := renderer.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, title, color, chunk.ShowHeader, chunk.Body, nil, enablePreview)
	appendReplyCardFooter(card, chunk.FooterLines)
	payload, err := json.Marshal(card)
	if err != nil {
		return false
	}
	if len(payload) > appdelivery.ReplyCardMaxPayloadBytes {
		return false
	}
	return appdelivery.CountCardComponentNodes(card) < appdelivery.ReplyCardMaxComponentCount
}
