package app

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) rewriteMarkdownPreviewText(ctx context.Context, sub *state.Submission, text string) string {
	text = strings.TrimSpace(text)
	if a == nil || a.feishu == nil || sub == nil || text == "" {
		return text
	}
	ws := config.FindWorkspace(a.cfg, sub.WorkspaceID)
	if ws == nil {
		return text
	}
	rewritten, err := a.feishu.RewriteMarkdownPreview(ctx, feishu.MarkdownPreviewRequest{
		Text:         text,
		WorkspaceCWD: ws.Cwd,
		ChatID:       sub.ChatID,
		UserID:       sub.UserID,
	})
	if err != nil {
		slog.Warn("markdown preview rewrite failed", "submission_id", sub.ID, "workspace_id", sub.WorkspaceID, "error", err)
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
		return normalizeCardMarkdown(a.rewriteMarkdownPreviewText(ctx, sub, text))
	}
	return a.prepareCardMarkdown(sub, text)
}
