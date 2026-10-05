package feishuapp

import (
	"context"
	domainsubmission "feidex/internal/domain/submission"
	"fmt"
	"strings"

	appcarddemo "feidex/internal/adapter/feishu/carddemo"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

type OutboundCardDemoOptions struct {
	Kind        string
	WorkspaceID string
	ChatID      string
	UserID      string
	Body        string
}

func BuildOutboundCardDemo(cfg *config.Config, opts OutboundCardDemoOptions) (map[string]any, string, error) {
	if cfg == nil {
		return nil, "", fmt.Errorf("nil config")
	}

	kind := appcarddemo.NormalizeKind(opts.Kind)
	if kind == "" {
		return nil, "", fmt.Errorf("unsupported outbound card demo kind %q", opts.Kind)
	}

	workspaceID := strings.TrimSpace(opts.WorkspaceID)
	if workspaceID == "" && len(cfg.Workspaces) > 0 {
		workspaceID = cfg.Workspaces[0].ID
	}
	body := strings.TrimSpace(opts.Body)
	if body == "" {
		body = appcarddemo.DefaultBody(kind)
	}

	a := &Frontend{
		cfg:    cfg,
		feishu: feishu.New(cfg.Feishu),
	}
	sub := &domainsubmission.Submission{
		WorkspaceID: workspaceID,
		ChatID:      strings.TrimSpace(opts.ChatID),
		UserID:      strings.TrimSpace(opts.UserID),
	}

	title, color, replyClass, showHeader := outboundMessageCardMeta(kind, workspaceID)
	if replyClass {
		card := newCardRenderer(a.Config()).renderReplyMarkdownCardWithHeaderOptions(context.Background(), sub, contentCardTitleForSubmission(a.State(), sub, title), color, showHeader, body, nil, kind == "final_message")
		return card, kind, nil
	}
	card := newCardRenderer(a.Config()).renderCompactMarkdownCard(sub, contentCardTitleForSubmission(a.State(), sub, title), color, "", body, nil)
	return card, kind, nil
}
