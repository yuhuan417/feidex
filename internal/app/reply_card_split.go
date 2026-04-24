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

func (a *App) fitReplyCardChunks(ctx context.Context, sub *state.Submission, title, color string, chunks []appdelivery.ReplyCardChunk, enablePreview bool) []appdelivery.ReplyCardChunk {
	if len(chunks) == 0 {
		return nil
	}
	fitted := make([]appdelivery.ReplyCardChunk, 0, len(chunks))
	for _, chunk := range chunks {
		fitted = append(fitted, a.expandReplyCardChunkToFit(ctx, sub, title, color, chunk, enablePreview)...)
	}
	return fitted
}

func (a *App) expandReplyCardChunkToFit(ctx context.Context, sub *state.Submission, title, color string, chunk appdelivery.ReplyCardChunk, enablePreview bool) []appdelivery.ReplyCardChunk {
	if a.replyCardChunkFits(ctx, sub, title, color, chunk, enablePreview) {
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
		if a.replyCardChunkFits(ctx, sub, title, color, candidate, enablePreview) {
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
			return a.replyCardChunkFits(ctx, sub, title, color, appdelivery.ReplyCardChunk{
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
	if a.replyCardChunkFits(ctx, sub, title, color, result[last], enablePreview) {
		return result
	}

	footerOnly := appdelivery.ReplyCardChunk{
		ShowHeader:  false,
		FooterLines: append([]string(nil), chunk.FooterLines...),
	}
	result[last].FooterLines = nil
	if a.replyCardChunkFits(ctx, sub, title, color, result[last], enablePreview) && a.replyCardChunkFits(ctx, sub, title, color, footerOnly, enablePreview) {
		return append(result, footerOnly)
	}
	result[last].FooterLines = append([]string(nil), chunk.FooterLines...)
	return result
}

func splitReplyTextBlockToFit(text string, fits func(string) bool) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if fits(text) {
		return []string{text}
	}
	for _, sep := range []string{"\n\n", "\n", " "} {
		idx := splitIndexNearMiddle(text, sep)
		if idx <= 0 {
			continue
		}
		left, right := splitReplyTextAt(text, idx, len(sep))
		if left == "" || right == "" {
			continue
		}
		return append(splitReplyTextBlockToFit(left, fits), splitReplyTextBlockToFit(right, fits)...)
	}
	return splitReplyTextByRunes(text, fits)
}

func splitReplyTextByRunes(text string, fits func(string) bool) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= 1 {
		return []string{strings.TrimSpace(text)}
	}
	mid := len(runes) / 2
	left := strings.TrimSpace(string(runes[:mid]))
	right := strings.TrimSpace(string(runes[mid:]))
	if left == "" || right == "" {
		return []string{strings.TrimSpace(text)}
	}
	return append(splitReplyTextBlockToFit(left, fits), splitReplyTextBlockToFit(right, fits)...)
}

func splitIndexNearMiddle(text, sep string) int {
	if sep == "" {
		return -1
	}
	mid := len(text) / 2
	if idx := strings.LastIndex(text[:mid], sep); idx >= 0 {
		return idx
	}
	if idx := strings.Index(text[mid:], sep); idx >= 0 {
		return mid + idx
	}
	return -1
}

func splitReplyTextAt(text string, index, sepLen int) (string, string) {
	if index < 0 || index > len(text) {
		return "", ""
	}
	left := strings.TrimSpace(text[:index])
	right := strings.TrimSpace(text[index+sepLen:])
	return left, right
}

func joinReplyChunkBodies(current, next string) string {
	current = strings.TrimSpace(current)
	next = strings.TrimSpace(next)
	switch {
	case current == "":
		return next
	case next == "":
		return current
	default:
		return current + "\n" + next
	}
}

func (a *App) replyCardChunkFits(ctx context.Context, sub *state.Submission, title, color string, chunk appdelivery.ReplyCardChunk, enablePreview bool) bool {
	card := a.cardRenderer().renderReplyMarkdownCardWithHeaderOptions(ctx, sub, title, color, chunk.ShowHeader, chunk.Body, nil, enablePreview)
	appendReplyCardFooter(card, chunk.FooterLines)
	payload, err := json.Marshal(card)
	if err != nil {
		return false
	}
	if len(payload) > feishuReplyCardMaxPayloadBytes {
		return false
	}
	return countCardComponentNodes(card) < feishuReplyCardMaxComponentCount
}

func countCardComponentNodes(value any) int {
	switch node := value.(type) {
	case map[string]any:
		count := 0
		if _, ok := node["tag"]; ok {
			count++
		}
		for _, child := range node {
			count += countCardComponentNodes(child)
		}
		return count
	case []map[string]any:
		count := 0
		for _, child := range node {
			count += countCardComponentNodes(child)
		}
		return count
	case []any:
		count := 0
		for _, child := range node {
			count += countCardComponentNodes(child)
		}
		return count
	default:
		return 0
	}
}
