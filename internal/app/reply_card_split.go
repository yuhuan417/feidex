package app

import (
	"context"
	"encoding/json"
	"strings"

	appdelivery "feidex/internal/app/delivery"
	"feidex/internal/state"
)

const feishuReplyCardMaxPayloadBytes = appdelivery.ReplyCardMaxPayloadBytes
const feishuReplyCardMaxComponentCount = appdelivery.ReplyCardMaxComponentCount

func fitReplyCardChunks(a *App, ctx context.Context, sub *state.Submission, title, color string, chunks []appdelivery.ReplyCardChunk, enablePreview bool) []appdelivery.ReplyCardChunk {
	if len(chunks) == 0 {
		return nil
	}
	fitted := make([]appdelivery.ReplyCardChunk, 0, len(chunks))
	for _, chunk := range chunks {
		fitted = append(fitted, expandReplyCardChunkToFit(a, ctx, sub, title, color, chunk, enablePreview)...)
	}
	return fitted
}

func expandReplyCardChunkToFit(a *App, ctx context.Context, sub *state.Submission, title, color string, chunk appdelivery.ReplyCardChunk, enablePreview bool) []appdelivery.ReplyCardChunk {
	if replyCardChunkFits(a, ctx, sub, title, color, chunk, enablePreview) {
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
		candidate.Body = joinReplyChunkBodies(current.Body, block.Text)
		if replyCardChunkFits(a, ctx, sub, title, color, candidate, enablePreview) {
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
		parts := splitReplyTextBlockToFit(block.Text, func(part string) bool {
			return replyCardChunkFits(a, ctx, sub, title, color, appdelivery.ReplyCardChunk{
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
	if replyCardChunkFits(a, ctx, sub, title, color, result[last], enablePreview) {
		return result
	}

	footerOnly := appdelivery.ReplyCardChunk{
		ShowHeader:  false,
		FooterLines: append([]string(nil), chunk.FooterLines...),
	}
	result[last].FooterLines = nil
	if replyCardChunkFits(a, ctx, sub, title, color, result[last], enablePreview) && replyCardChunkFits(a, ctx, sub, title, color, footerOnly, enablePreview) {
		return append(result, footerOnly)
	}
	result[last].FooterLines = append([]string(nil), chunk.FooterLines...)
	return result
}

var splitReplyTextBlockToFit = appdelivery.SplitReplyTextBlockToFit

var splitReplyTextByRunes = appdelivery.SplitReplyTextByRunes

var splitIndexNearMiddle = appdelivery.SplitIndexNearMiddle

var splitReplyTextAt = appdelivery.SplitReplyTextAt

var joinReplyChunkBodies = appdelivery.JoinReplyChunkBodies

func replyCardChunkFits(a *App, ctx context.Context, sub *state.Submission, title, color string, chunk appdelivery.ReplyCardChunk, enablePreview bool) bool {
	card := cardRendererForApp(a).renderReplyMarkdownCardWithHeaderOptions(ctx, sub, title, color, chunk.ShowHeader, chunk.Body, nil, enablePreview)
	appendReplyCardFooter(card, chunk.FooterLines)
	payload, err := json.Marshal(card)
	if err != nil {
		return false
	}
	if len(payload) > feishuReplyCardMaxPayloadBytes {
		return false
	}
	return appdelivery.CountCardComponentNodes(card) < feishuReplyCardMaxComponentCount
}
