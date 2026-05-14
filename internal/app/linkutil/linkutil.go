// Package linkutil provides pure markdown link processing and fence-parsing
// functions extracted from the app package.
package linkutil

import (
	"net/url"
	"regexp"
	"strings"

	appattachments "feidex/internal/app/attachments"
)

const (
	FenceMinLen       = 3
	FencePreferredLen = 4
)

var InlineCodeLocalPreviewTargetRe = regexp.MustCompile("`([^`\n]+)`")

func NormalizeCardMarkdown(text string) string {
	return strings.TrimSpace(text)
}

func LinkifyInlineCodeURLs(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	inFence := false
	openFenceLen := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if run, info, ok := ParseBacktickFenceLine(trimmed); ok {
			if !inFence {
				inFence = true
				openFenceLen = run
			} else if info == "" && run >= openFenceLen {
				inFence = false
				openFenceLen = 0
			}
			continue
		}
		if inFence {
			continue
		}
		lines[i] = LinkifyInlineCodeURLsLine(line)
	}
	return strings.Join(lines, "\n")
}

func ParseBacktickFenceLine(trimmed string) (run int, info string, ok bool) {
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" || trimmed[0] != '`' {
		return 0, "", false
	}
	run = CountLeadingBackticks(trimmed)
	if run < FenceMinLen {
		return 0, "", false
	}
	info = strings.TrimSpace(trimmed[run:])
	return run, info, true
}

func CountLeadingBackticks(s string) int {
	n := 0
	for n < len(s) && s[n] == '`' {
		n++
	}
	return n
}

func NormalizeLocalFilePreviewTargets(text, workspaceCwd string) string {
	text = strings.TrimSpace(text)
	workspaceCwd = strings.TrimSpace(workspaceCwd)
	if text == "" || workspaceCwd == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	inFence := false
	openFenceLen := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if run, info, ok := ParseBacktickFenceLine(trimmed); ok {
			if !inFence {
				inFence = true
				openFenceLen = run
			} else if info == "" && run >= openFenceLen {
				inFence = false
				openFenceLen = 0
			}
			continue
		}
		if inFence {
			continue
		}
		lines[i] = LinkifyInlineCodeLocalFileRefsLine(line, workspaceCwd)
	}
	return strings.Join(lines, "\n")
}

func LinkifyInlineCodeLocalFileRefsLine(line, workspaceCwd string) string {
	matches := InlineCodeLocalPreviewTargetRe.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return line
	}
	linkRanges := appattachments.MarkdownLinkFullRe.FindAllStringIndex(line, -1)
	var builder strings.Builder
	last := 0
	changed := false
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		start := match[0]
		end := match[1]
		targetStart := match[2]
		targetEnd := match[3]
		builder.WriteString(line[last:start])
		original := line[start:end]
		if RangeWithinAny(start, end, linkRanges) {
			builder.WriteString(original)
			last = end
			continue
		}
		rawTarget := strings.TrimSpace(line[targetStart:targetEnd])
		displayPath, ok := appattachments.LocalLinkDisplayTarget(rawTarget, workspaceCwd)
		if !ok {
			builder.WriteString(original)
			last = end
			continue
		}
		replacement := "[" + EscapeMarkdownLinkLabel(displayPath) + "](" + rawTarget + ")"
		if replacement != original {
			changed = true
		}
		builder.WriteString(replacement)
		last = end
	}
	builder.WriteString(line[last:])
	if !changed {
		return line
	}
	return builder.String()
}

func LinkifyInlineCodeURLsLine(line string) string {
	matches := InlineCodeLocalPreviewTargetRe.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return line
	}
	linkRanges := appattachments.MarkdownLinkFullRe.FindAllStringIndex(line, -1)
	var builder strings.Builder
	last := 0
	changed := false
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		start := match[0]
		end := match[1]
		targetStart := match[2]
		targetEnd := match[3]
		builder.WriteString(line[last:start])
		original := line[start:end]
		if RangeWithinAny(start, end, linkRanges) {
			builder.WriteString(original)
			last = end
			continue
		}
		rawTarget := strings.TrimSpace(line[targetStart:targetEnd])
		urlTarget, ok := InlineCodeURLTarget(rawTarget)
		if !ok {
			builder.WriteString(original)
			last = end
			continue
		}
		replacement := "[" + EscapeMarkdownLinkLabel(urlTarget) + "](" + urlTarget + ")"
		if replacement != original {
			changed = true
		}
		builder.WriteString(replacement)
		last = end
	}
	builder.WriteString(line[last:])
	if !changed {
		return line
	}
	return builder.String()
}

func InlineCodeURLTarget(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "\"'")
	if raw == "" {
		return "", false
	}
	if strings.HasPrefix(raw, "<") && strings.HasSuffix(raw, ">") {
		raw = strings.TrimPrefix(strings.TrimSuffix(raw, ">"), "<")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", false
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", false
	}
	return raw, true
}

func RangeWithinAny(start, end int, ranges [][]int) bool {
	for _, r := range ranges {
		if len(r) < 2 {
			continue
		}
		if start >= r[0] && end <= r[1] {
			return true
		}
	}
	return false
}

func EscapeMarkdownLinkLabel(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, `[`, `\[`)
	value = strings.ReplaceAll(value, `]`, `\]`)
	return value
}
