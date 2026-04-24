package app

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

var inlineCodeLocalPreviewTargetRe = regexp.MustCompile("`([^`\n]+)`")

func (a *App) rewriteLocalFileLinksText(ctx context.Context, sub *state.Submission, text string) string {
	text = strings.TrimSpace(text)
	if a == nil || a.feishu == nil || sub == nil || text == "" {
		return text
	}
	ws := config.FindWorkspace(a.cfg, sub.WorkspaceID)
	if ws == nil {
		return text
	}
	normalized := normalizeLocalFilePreviewTargets(text, ws.Cwd)
	rewritten, err := a.feishu.RewriteLocalFileLinks(ctx, feishu.LocalFileLinkRewriteRequest{
		Text:         normalized,
		WorkspaceCWD: ws.Cwd,
		ChatID:       sub.ChatID,
		UserID:       sub.UserID,
	})
	if err != nil {
		slog.Warn("local file link rewrite failed", "submission_id", sub.ID, "workspace_id", sub.WorkspaceID, "error", err)
	}
	if strings.TrimSpace(rewritten) == "" {
		return text
	}
	rewritten = strings.TrimSpace(rewritten)
	if rewritten == strings.TrimSpace(normalized) && normalized != text {
		return text
	}
	return rewritten
}

func normalizeLocalFilePreviewTargets(text, workspaceCwd string) string {
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
		if run, info, ok := parseBacktickFenceLine(trimmed); ok {
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
		lines[i] = linkifyInlineCodeLocalFileRefsLine(line, workspaceCwd)
	}
	return strings.Join(lines, "\n")
}

func linkifyInlineCodeLocalFileRefsLine(line, workspaceCwd string) string {
	matches := inlineCodeLocalPreviewTargetRe.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return line
	}
	linkRanges := markdownLinkFullRe.FindAllStringIndex(line, -1)
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
		if rangeWithinAny(start, end, linkRanges) {
			builder.WriteString(original)
			last = end
			continue
		}
		rawTarget := strings.TrimSpace(line[targetStart:targetEnd])
		displayPath, ok := localLinkDisplayTarget(rawTarget, workspaceCwd)
		if !ok {
			builder.WriteString(original)
			last = end
			continue
		}
		replacement := "[" + escapeMarkdownLinkLabel(displayPath) + "](" + rawTarget + ")"
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

func rangeWithinAny(start, end int, ranges [][]int) bool {
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

func escapeMarkdownLinkLabel(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, `[`, `\[`)
	value = strings.ReplaceAll(value, `]`, `\]`)
	return value
}

func (a *App) prepareReplyCardMarkdown(ctx context.Context, sub *state.Submission, text string, enablePreview bool) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if enablePreview {
		if sub != nil {
			if ws := config.FindWorkspace(a.cfg, sub.WorkspaceID); ws != nil {
				text = neutralizeLocalMarkdownLinks(text, ws.Cwd)
			}
		}
		return normalizeCardMarkdown(text)
	}
	return a.cardRenderer().prepareCardMarkdown(sub, text)
}

func (a *App) scheduleLocalFileLinkPatch(sub *state.Submission, messageID, title, color string, showHeader bool, body string, footerLines []string) {
	messageID = strings.TrimSpace(messageID)
	body = strings.TrimSpace(body)
	if a == nil || a.feishu == nil || sub == nil || messageID == "" || body == "" {
		return
	}
	managed := newFinalCardPatchService(a).markFinalCardPreviewPending(messageID)
	go func() {
		if managed {
			defer newFinalCardPatchService(a).markFinalCardPreviewDone(messageID)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		rewritten := a.rewriteLocalFileLinksText(ctx, sub, body)
		if strings.TrimSpace(rewritten) == "" || strings.TrimSpace(rewritten) == body {
			return
		}
		if managed && newFinalCardPatchService(a).updateFinalCardPatchBody(messageID, rewritten) {
			return
		}
		card := a.cardRenderer().renderReplyMarkdownCardWithHeaderOptions(context.Background(), sub, title, color, showHeader, rewritten, nil, true)
		appendReplyCardFooter(card, footerLines)
		patchCtx, patchCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer patchCancel()
		if err := a.feishu.PatchCard(patchCtx, messageID, card); err != nil {
			slog.Warn("local file link patch failed",
				"submission_id", sub.ID,
				"workspace_id", sub.WorkspaceID,
				"message_id", messageID,
				"error", err,
			)
		}
	}()
}
