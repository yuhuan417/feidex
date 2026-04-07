package app

import (
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func newMarkdownBodyCard(title, color string) map[string]any {
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
	if strings.TrimSpace(title) != "" {
		card["header"] = map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": strings.TrimSpace(title),
			},
			"template": color,
		}
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
	actions := make([]map[string]any, 0, len(buttons))
	for _, btn := range buttons {
		action := map[string]any{
			"tag":   "button",
			"type":  btn.Type,
			"text":  map[string]any{"tag": "plain_text", "content": btn.Text},
			"value": btn.Value,
		}
		if strings.TrimSpace(btn.Name) != "" {
			action["name"] = btn.Name
		}
		actions = append(actions, action)
	}
	return map[string]any{
		"tag":     "action",
		"actions": actions,
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
	card := newMarkdownBodyCard(title, color)
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
