package feishuapp

import (
	"context"
	domainsubmission "feidex/internal/domain/submission"
	"strings"

	appcards "feidex/internal/adapter/feishu/cards"

	applinkutil "feidex/internal/adapter/feishu/linkutil"
	"feidex/internal/feishu"
)

type cardRenderer struct {
	app *App
}

func cardRendererForApp(a *App) cardRenderer {
	return cardRenderer{app: a}
}

func (r cardRenderer) prepareCardMarkdown(sub *domainsubmission.Submission, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if sub == nil || r.app == nil {
		return applinkutil.NormalizeCardMarkdown(text)
	}
	return prepareSubmissionCardMarkdown(r.app.cfg, sub, text)
}

func (r cardRenderer) renderReplyMarkdownCard(sub *domainsubmission.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	return r.renderReplyMarkdownCardWithOptions(context.Background(), sub, title, color, body, buttons, false)
}

func (r cardRenderer) renderReplyMarkdownCardWithOptions(ctx context.Context, sub *domainsubmission.Submission, title, color, body string, buttons []feishu.Button, enablePreview bool) map[string]any {
	return r.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, title, color, strings.TrimSpace(title) != "", body, buttons, enablePreview)
}

func (r cardRenderer) renderReplyMarkdownCardWithHeaderOptions(ctx context.Context, sub *domainsubmission.Submission, title, color string, showHeader bool, body string, buttons []feishu.Button, enablePreview bool) map[string]any {
	card := appcards.NewMarkdownBodyCardWithHeader(title, color, showHeader)
	if r.app == nil {
		if content := applinkutil.NormalizeCardMarkdown(body); content != "" {
			appcards.AppendMarkdownBodyCardElement(card, map[string]any{
				"tag":     "markdown",
				"content": content,
			})
		}
	} else if content := prepareReplyCardMarkdown(r.app, ctx, sub, body, enablePreview); content != "" {
		appcards.AppendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": content,
		})
	}
	for _, row := range appcards.BuildMarkdownBodyCardActionElements(buttons) {
		appcards.AppendMarkdownBodyCardElement(card, row)
	}
	if bodyElements, _ := card["body"].(map[string]any)["elements"].([]map[string]any); len(bodyElements) == 0 {
		appcards.AppendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": " ",
		})
	}
	return card
}

func (r cardRenderer) renderCompactMarkdownCard(sub *domainsubmission.Submission, title, color, meta, body string, buttons []feishu.Button) map[string]any {
	card := appcards.NewMarkdownBodyCard(title, color)
	meta = strings.Join(strings.Fields(strings.TrimSpace(meta)), " ")
	if meta != "" {
		appcards.AppendMarkdownBodyCardElement(card, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":        "plain_text",
				"content":    meta,
				"text_size":  "notation",
				"text_color": "grey",
			},
		})
	}
	if content := r.prepareCardMarkdown(sub, body); content != "" {
		appcards.AppendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": content,
		})
	}
	for _, row := range appcards.BuildMarkdownBodyCardActionElements(buttons) {
		appcards.AppendMarkdownBodyCardElement(card, row)
	}
	if bodyElements, _ := card["body"].(map[string]any)["elements"].([]map[string]any); len(bodyElements) == 0 {
		appcards.AppendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": " ",
		})
	}
	return card
}
