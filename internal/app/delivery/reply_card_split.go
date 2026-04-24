package delivery

import (
	"regexp"
	"strings"
)

const ReplyCardMaxTables = 5
const ReplyCardMaxPayloadBytes = 20000
const ReplyCardMaxComponentCount = 200

var markdownTableSeparatorLineRe = regexp.MustCompile(`^\s*\|?(?:\s*:?-{3,}:?\s*\|)+(?:\s*:?-{3,}:?\s*)\|?\s*$`)

type MarkdownSplitBlock struct {
	Text       string
	TableCount int
}

type ReplyCardChunk struct {
	Body        string
	FooterLines []string
	ShowHeader  bool
}

type SentReplyChunk struct {
	MessageID   string
	CardID      string
	Title       string
	Body        string
	FooterLines []string
	ShowHeader  bool
}

func BuildReplyCardChunks(body string, showHeader bool, footerLines []string) []ReplyCardChunk {
	parts := SplitMarkdownByTableLimit(body, ReplyCardMaxTables)
	if len(parts) == 0 {
		return nil
	}
	chunks := make([]ReplyCardChunk, 0, len(parts))
	for i, part := range parts {
		chunk := ReplyCardChunk{Body: part, ShowHeader: showHeader && i == 0}
		if i == len(parts)-1 {
			chunk.FooterLines = append([]string(nil), footerLines...)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func SplitMarkdownByTableLimit(text string, maxTables int) []string {
	text = normalizeMarkdownForSplit(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	if maxTables <= 0 {
		return []string{text}
	}
	blocks := SplitMarkdownBlocks(text)
	if len(blocks) == 0 {
		return []string{text}
	}
	var parts []string
	var current []string
	tableCount := 0
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

func SplitMarkdownBlocks(text string) []MarkdownSplitBlock {
	lines := strings.Split(normalizeMarkdownForSplit(strings.TrimSpace(text)), "\n")
	if len(lines) == 0 {
		return nil
	}
	blocks := make([]MarkdownSplitBlock, 0, len(lines))
	current := make([]string, 0, len(lines))
	flushCurrent := func() {
		if len(current) == 0 {
			return
		}
		blocks = append(blocks, MarkdownSplitBlock{Text: strings.Join(current, "\n")})
		current = nil
	}
	fenceOpen := false
	openFenceLen := 0
	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if !fenceOpen && IsMarkdownTableStart(lines, i) {
			flushCurrent()
			tableLines := []string{lines[i], lines[i+1]}
			i += 2
			for i < len(lines) {
				next := strings.TrimSpace(lines[i])
				if next == "" || IsFenceBoundaryForSplit(next) || !strings.Contains(lines[i], "|") {
					break
				}
				tableLines = append(tableLines, lines[i])
				i++
			}
			blocks = append(blocks, MarkdownSplitBlock{Text: strings.Join(tableLines, "\n"), TableCount: 1})
			continue
		}
		current = append(current, line)
		if !fenceOpen {
			if run, _, ok := parseBacktickFenceLine(trimmed); ok {
				fenceOpen = true
				openFenceLen = run
			}
		} else if IsFenceCloseForSplit(trimmed, openFenceLen) {
			fenceOpen = false
			openFenceLen = 0
		}
		i++
	}
	flushCurrent()
	return blocks
}

func IsMarkdownTableStart(lines []string, index int) bool {
	if index < 0 || index+1 >= len(lines) {
		return false
	}
	header := strings.TrimSpace(lines[index])
	separator := strings.TrimSpace(lines[index+1])
	if header == "" || separator == "" {
		return false
	}
	if IsFenceBoundaryForSplit(header) || IsFenceBoundaryForSplit(separator) {
		return false
	}
	if !strings.Contains(header, "|") {
		return false
	}
	return markdownTableSeparatorLineRe.MatchString(separator)
}

func normalizeMarkdownForSplit(text string) string { return strings.TrimSpace(text) }

func IsFenceBoundaryForSplit(trimmed string) bool {
	_, _, ok := parseBacktickFenceLine(trimmed)
	return ok
}

func IsFenceCloseForSplit(trimmed string, openLen int) bool {
	run, info, ok := parseBacktickFenceLine(trimmed)
	return ok && info == "" && run >= openLen
}

func parseBacktickFenceLine(trimmed string) (run int, info string, ok bool) {
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" || trimmed[0] != '`' {
		return 0, "", false
	}
	for run < len(trimmed) && trimmed[run] == '`' {
		run++
	}
	if run < 3 {
		return 0, "", false
	}
	return run, strings.TrimSpace(trimmed[run:]), true
}
