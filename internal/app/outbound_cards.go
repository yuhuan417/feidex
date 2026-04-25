package app

import (
	"context"
	"strings"

	appcards "feidex/internal/app/cards"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type cardRenderer struct {
	app *App
}

func cardRendererForApp(a *App) cardRenderer {
	return cardRenderer{app: a}
}

func (r cardRenderer) prepareCardMarkdown(sub *state.Submission, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if sub == nil || r.app == nil {
		return normalizeCardMarkdown(text)
	}
	return prepareSubmissionCardMarkdown(r.app, sub, text)
}

func (r cardRenderer) renderReplyMarkdownCard(sub *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	return r.renderReplyMarkdownCardWithOptions(context.Background(), sub, title, color, body, buttons, false)
}

func (r cardRenderer) renderReplyMarkdownCardWithOptions(ctx context.Context, sub *state.Submission, title, color, body string, buttons []feishu.Button, enablePreview bool) map[string]any {
	return r.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, title, color, strings.TrimSpace(title) != "", body, buttons, enablePreview)
}

func (r cardRenderer) renderReplyMarkdownCardWithHeaderOptions(ctx context.Context, sub *state.Submission, title, color string, showHeader bool, body string, buttons []feishu.Button, enablePreview bool) map[string]any {
	card := appcards.NewMarkdownBodyCardWithHeader(title, color, showHeader)
	if r.app == nil {
		if content := normalizeCardMarkdown(body); content != "" {
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

func (r cardRenderer) renderCompactMarkdownCard(sub *state.Submission, title, color, meta, body string, buttons []feishu.Button) map[string]any {
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
