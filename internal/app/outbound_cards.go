package app

import (
	"context"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func newMarkdownBodyCard(title, color string) map[string]any {
	return newMarkdownBodyCardWithHeader(title, color, strings.TrimSpace(title) != "")
}

func newMarkdownBodyCardWithHeader(title, color string, showHeader bool) map[string]any {
	if strings.TrimSpace(color) == "" {
		color = "blue"
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"wide_screen_mode": true,
			"update_multi":     true,
		},
		"body": map[string]any{
			"elements": []map[string]any{},
		},
	}
	if showHeader {
		headerTitle := strings.TrimSpace(title)
		if headerTitle == "" {
			headerTitle = " "
		}
		header := map[string]any{"template": color}
		header["title"] = map[string]any{
			"tag":     "plain_text",
			"content": headerTitle,
		}
		card["header"] = header
	}
	return card
}

func appendMarkdownBodyCardElement(card map[string]any, elem map[string]any) {
	body, _ := card["body"].(map[string]any)
	if body == nil {
		body = map[string]any{"elements": []map[string]any{}}
		card["body"] = body
	}
	elements, _ := body["elements"].([]map[string]any)
	body["elements"] = append(elements, elem)
}

func buildMarkdownBodyCardActionElement(buttons []feishu.Button) map[string]any {
	columns := make([]map[string]any, 0, len(buttons))
	for _, btn := range buttons {
		button := map[string]any{
			"tag":  "button",
			"type": firstNonEmpty(strings.TrimSpace(btn.Type), "default"),
			"text": map[string]any{"tag": "plain_text", "content": btn.Text},
			"behaviors": []map[string]any{{
				"type":  "callback",
				"value": btn.Value,
			}},
		}
		if strings.TrimSpace(btn.Name) != "" {
			button["name"] = btn.Name
		}
		columns = append(columns, map[string]any{
			"tag":    "column",
			"width":  "weighted",
			"weight": 1,
			"elements": []map[string]any{
				button,
			},
		})
	}
	return map[string]any{
		"tag":                "column_set",
		"horizontal_spacing": "8px",
		"columns":            columns,
	}
}

func (a *App) prepareCardMarkdown(sub *state.Submission, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if sub == nil {
		return normalizeCardMarkdown(text)
	}
	return a.prepareSubmissionCardMarkdown(sub, text)
}

func (a *App) renderReplyMarkdownCard(sub *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any {
	return a.renderReplyMarkdownCardWithOptions(context.Background(), sub, title, color, body, buttons, false)
}

func (a *App) renderReplyMarkdownCardWithOptions(ctx context.Context, sub *state.Submission, title, color, body string, buttons []feishu.Button, enablePreview bool) map[string]any {
	return a.renderReplyMarkdownCardWithHeaderOptions(ctx, sub, title, color, strings.TrimSpace(title) != "", body, buttons, enablePreview)
}

func (a *App) renderReplyMarkdownCardWithHeaderOptions(ctx context.Context, sub *state.Submission, title, color string, showHeader bool, body string, buttons []feishu.Button, enablePreview bool) map[string]any {
	card := newMarkdownBodyCardWithHeader(title, color, showHeader)
	if content := a.prepareReplyCardMarkdown(ctx, sub, body, enablePreview); content != "" {
		appendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": content,
		})
	}
	if len(buttons) > 0 {
		appendMarkdownBodyCardElement(card, buildMarkdownBodyCardActionElement(buttons))
	}
	if bodyElements, _ := card["body"].(map[string]any)["elements"].([]map[string]any); len(bodyElements) == 0 {
		appendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": " ",
		})
	}
	return card
}

func (a *App) renderCompactMarkdownCard(sub *state.Submission, title, color, meta, body string, buttons []feishu.Button) map[string]any {
	card := newMarkdownBodyCard(title, color)
	meta = strings.Join(strings.Fields(strings.TrimSpace(meta)), " ")
	if meta != "" {
		appendMarkdownBodyCardElement(card, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":        "plain_text",
				"content":    meta,
				"text_size":  "notation",
				"text_color": "grey",
			},
		})
	}
	if content := a.prepareCardMarkdown(sub, body); content != "" {
		appendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": content,
		})
	}
	if len(buttons) > 0 {
		appendMarkdownBodyCardElement(card, buildMarkdownBodyCardActionElement(buttons))
	}
	if bodyElements, _ := card["body"].(map[string]any)["elements"].([]map[string]any); len(bodyElements) == 0 {
		appendMarkdownBodyCardElement(card, map[string]any{
			"tag":     "markdown",
			"content": " ",
		})
	}
	return card
}
