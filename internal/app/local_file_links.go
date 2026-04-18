package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) rewriteLocalFileLinksText(ctx context.Context, sub *state.Submission, text string) string {
	text = strings.TrimSpace(text)
	if a == nil || a.feishu == nil || sub == nil || text == "" {
		return text
	}
	ws := config.FindWorkspace(a.cfg, sub.WorkspaceID)
	if ws == nil {
		return text
	}
	rewritten, err := a.feishu.RewriteLocalFileLinks(ctx, feishu.LocalFileLinkRewriteRequest{
		Text:         text,
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
	return strings.TrimSpace(rewritten)
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
	return a.prepareCardMarkdown(sub, text)
}

func (a *App) scheduleLocalFileLinkPatch(sub *state.Submission, messageID, title, color string, showHeader bool, body string, footerLines []string) {
	messageID = strings.TrimSpace(messageID)
	body = strings.TrimSpace(body)
	if a == nil || a.feishu == nil || sub == nil || messageID == "" || body == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		rewritten := a.rewriteLocalFileLinksText(ctx, sub, body)
		if strings.TrimSpace(rewritten) == "" || strings.TrimSpace(rewritten) == body {
			return
		}
		card := a.renderReplyMarkdownCardWithHeaderOptions(context.Background(), sub, title, color, showHeader, rewritten, nil, true)
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
