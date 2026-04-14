package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"feidex/internal/state"
)

const feishuReplyCardMaxTables = 5
const feishuReplyCardMaxPayloadBytes = 20000
const feishuReplyCardMaxComponentCount = 200

var markdownTableSeparatorLineRe = regexp.MustCompile(`^\s*\|?(?:\s*:?-{3,}:?\s*\|)+(?:\s*:?-{3,}:?\s*)\|?\s*$`)

type markdownSplitBlock struct {
	Text       string
	TableCount int
}

type replyCardChunk struct {
	Body        string
	FooterLines []string
	ShowHeader  bool
}

type sentReplyChunk struct {
	MessageID   string
	CardID      string
	Title       string
	Body        string
	FooterLines []string
	ShowHeader  bool
}

func buildReplyCardChunks(body string, showHeader bool, footerLines []string) []replyCardChunk {
	parts := splitMarkdownByTableLimit(body, feishuReplyCardMaxTables)
	if len(parts) == 0 {
		return nil
	}
	chunks := make([]replyCardChunk, 0, len(parts))
	for i, part := range parts {
		chunk := replyCardChunk{
			Body:       part,
			ShowHeader: showHeader && i == 0,
		}
		if i == len(parts)-1 {
			chunk.FooterLines = append([]string(nil), footerLines...)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func splitMarkdownByTableLimit(text string, maxTables int) []string {
	text = normalizeMarkdownForSplit(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	if maxTables <= 0 {
		return []string{text}
	}

	blocks := splitMarkdownBlocks(text)
	if len(blocks) == 0 {
		return []string{text}
	}

	var (
		parts      []string
		current    []string
		tableCount int
	)
	flush := func() {
		if len(current) == 0 {
			return
		}
		part := strings.TrimSpace(strings.Join(current, "\n"))
		if part != "" {
			parts = append(parts, part)
		}
		current = nil
		tableCount = 0
	}

	for _, block := range blocks {
		if block.TableCount > 0 && tableCount+block.TableCount > maxTables && len(current) > 0 {
			flush()
		}
		if strings.TrimSpace(block.Text) == "" && len(current) == 0 {
			continue
		}
		current = append(current, block.Text)
		tableCount += block.TableCount
	}
	flush()

	if len(parts) == 0 {
		return []string{text}
	}
	return parts
}

func splitMarkdownBlocks(text string) []markdownSplitBlock {
	lines := strings.Split(normalizeMarkdownForSplit(strings.TrimSpace(text)), "\n")
	if len(lines) == 0 {
		return nil
	}

	blocks := make([]markdownSplitBlock, 0, len(lines))
	current := make([]string, 0, len(lines))
	flushCurrent := func() {
		if len(current) == 0 {
			return
		}
		blocks = append(blocks, markdownSplitBlock{Text: strings.Join(current, "\n")})
		current = nil
	}

	fenceOpen := false
	openFenceLen := 0
	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if !fenceOpen && isMarkdownTableStart(lines, i) {
			flushCurrent()
			tableLines := []string{lines[i], lines[i+1]}
			i += 2
			for i < len(lines) {
				next := strings.TrimSpace(lines[i])
				if next == "" || isFenceBoundaryForSplit(next) || !strings.Contains(lines[i], "|") {
					break
				}
				tableLines = append(tableLines, lines[i])
				i++
			}
			blocks = append(blocks, markdownSplitBlock{
				Text:       strings.Join(tableLines, "\n"),
				TableCount: 1,
			})
			continue
		}

		current = append(current, line)
		if !fenceOpen {
			if run, _, ok := parseBacktickFenceLine(trimmed); ok {
				fenceOpen = true
				openFenceLen = run
			}
		} else if isFenceCloseForSplit(trimmed, openFenceLen) {
			fenceOpen = false
			openFenceLen = 0
		}
		i++
	}
	flushCurrent()
	return blocks
}

func isMarkdownTableStart(lines []string, index int) bool {
	if index < 0 || index+1 >= len(lines) {
		return false
	}
	header := strings.TrimSpace(lines[index])
	separator := strings.TrimSpace(lines[index+1])
	if header == "" || separator == "" {
		return false
	}
	if isFenceBoundaryForSplit(header) || isFenceBoundaryForSplit(separator) {
		return false
	}
	if !strings.Contains(header, "|") {
		return false
	}
	return markdownTableSeparatorLineRe.MatchString(separator)
}

func normalizeMarkdownForSplit(text string) string {
	return strings.TrimSpace(text)
}

func isFenceBoundaryForSplit(trimmed string) bool {
	_, _, ok := parseBacktickFenceLine(trimmed)
	return ok
}

func isFenceCloseForSplit(trimmed string, openLen int) bool {
	run, info, ok := parseBacktickFenceLine(trimmed)
	return ok && info == "" && run >= openLen
}

func (a *App) fitReplyCardChunks(ctx context.Context, sub *state.Submission, title, color string, chunks []replyCardChunk, enablePreview bool) []replyCardChunk {
	if len(chunks) == 0 {
		return nil
	}
	fitted := make([]replyCardChunk, 0, len(chunks))
	for _, chunk := range chunks {
		fitted = append(fitted, a.expandReplyCardChunkToFit(ctx, sub, title, color, chunk, enablePreview)...)
	}
	return fitted
}

func (a *App) expandReplyCardChunkToFit(ctx context.Context, sub *state.Submission, title, color string, chunk replyCardChunk, enablePreview bool) []replyCardChunk {
	if a.replyCardChunkFits(ctx, sub, title, color, chunk, enablePreview) {
		return []replyCardChunk{chunk}
	}

	blocks := splitMarkdownBlocks(chunk.Body)
	if len(blocks) == 0 {
		return []replyCardChunk{chunk}
	}

	result := make([]replyCardChunk, 0, len(blocks))
	current := replyCardChunk{ShowHeader: chunk.ShowHeader}
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
			current = replyCardChunk{ShowHeader: false}
			blocks = append([]markdownSplitBlock{block}, blocks...)
			continue
		}
		if block.TableCount > 0 {
			current.Body = strings.TrimSpace(block.Text)
			result = append(result, current)
			current = replyCardChunk{ShowHeader: false}
			continue
		}
		parts := splitReplyTextBlockToFit(block.Text, func(part string) bool {
			return a.replyCardChunkFits(ctx, sub, title, color, replyCardChunk{
				Body:       part,
				ShowHeader: current.ShowHeader,
			}, enablePreview)
		})
		if len(parts) <= 1 {
			current.Body = strings.TrimSpace(block.Text)
			result = append(result, current)
			current = replyCardChunk{ShowHeader: false}
			continue
		}
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			result = append(result, replyCardChunk{
				Body:       part,
				ShowHeader: current.ShowHeader,
			})
			current = replyCardChunk{ShowHeader: false}
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

	footerOnly := replyCardChunk{
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

func (a *App) replyCardChunkFits(ctx context.Context, sub *state.Submission, title, color string, chunk replyCardChunk, enablePreview bool) bool {
	card := a.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, title, color, chunk.ShowHeader, chunk.Body, nil, enablePreview)
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
