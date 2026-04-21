package app

type selectStaticOption struct {
	Text  string
	Value string
}

func buildSelectStaticElement(name, placeholder string, actionValue map[string]any, options []selectStaticOption, initialOption string) map[string]any {
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
		"behaviors": []map[string]any{{
			"type":  "callback",
			"value": actionValue,
		}},
	}
	if initialOption != "" {
		element["initial_option"] = initialOption
	}
	return element
}

func buildFormSelectStaticElement(name, placeholder string, options []selectStaticOption, initialOption string) map[string]any {
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
