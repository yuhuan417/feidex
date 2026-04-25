package cards

import (
	"strings"

	"feidex/internal/app/apputil"
	"feidex/internal/feishu"
)

type SelectStaticOption struct {
	Text  string
	Value string
}

func NewMarkdownBodyCard(title, color string) map[string]any {
	return NewMarkdownBodyCardWithHeader(title, color, strings.TrimSpace(title) != "")
}

func NewMarkdownBodyCardWithHeader(title, color string, showHeader bool) map[string]any {
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

func AppendMarkdownBodyCardElement(card map[string]any, elem map[string]any) {
	body, _ := card["body"].(map[string]any)
	if body == nil {
		body = map[string]any{"elements": []map[string]any{}}
		card["body"] = body
	}
	elements, _ := body["elements"].([]map[string]any)
	body["elements"] = append(elements, elem)
}

func BuildMarkdownBodyCardActionElement(buttons []feishu.Button) map[string]any {
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

func BuildMarkdownBodyCardActionElements(buttons []feishu.Button) []map[string]any {
	if len(buttons) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(buttons))
	for _, btn := range buttons {
		rows = append(rows, BuildMarkdownBodyCardActionElement([]feishu.Button{btn}))
	}
	return rows
}

func BuildSelectStaticElement(name, placeholder string, actionValue map[string]any, options []SelectStaticOption, initialOption string) map[string]any {
	element := buildSelectStaticBase(name, placeholder, options, initialOption)
	element["behaviors"] = []map[string]any{{
		"type":  "callback",
		"value": actionValue,
	}}
	return element
}

func BuildFormSelectStaticElement(name, placeholder string, options []SelectStaticOption, initialOption string) map[string]any {
	return buildSelectStaticBase(name, placeholder, options, initialOption)
}

func buildSelectStaticBase(name, placeholder string, options []SelectStaticOption, initialOption string) map[string]any {
	cardOptions := make([]map[string]any, 0, len(options))
	for _, option := range options {
		cardOptions = append(cardOptions, map[string]any{
			"text":  map[string]any{"tag": "plain_text", "content": option.Text},
			"value": option.Value,
		})
	}
	element := map[string]any{
		"tag":         "select_static",
		"name":        name,
		"placeholder": map[string]any{"tag": "plain_text", "content": placeholder},
		"options":     cardOptions,
	}
	if initialOption != "" {
		element["initial_option"] = initialOption
	}
	return element
}

func firstNonEmpty(values ...string) string {
	return apputil.FirstNonEmpty(values...)
}
