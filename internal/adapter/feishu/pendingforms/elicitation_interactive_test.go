package pendingforms

import (
	"strings"
	"testing"
)

func TestRenderElicitationRequestCardModes(t *testing.T) {
	inlinePayload := ElicitationFormPayload{
		Message: "Configure deployment",
		Schema: map[string]any{
			"type":     "object",
			"required": []any{"name", "tags"},
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"title":       "Project Name",
					"description": "Name shown to users",
					"minLength":   2.0,
				},
				"enabled": map[string]any{
					"type":    "boolean",
					"default": true,
				},
				"mode": map[string]any{
					"type": "string",
					"oneOf": []any{
						map[string]any{"const": "fast", "title": "Fast"},
						map[string]any{"const": "safe", "title": "Safe"},
					},
				},
				"tags": map[string]any{
					"type":     "array",
					"minItems": 1.0,
					"items": map[string]any{
						"anyOf": []any{
							map[string]any{"const": "alpha", "title": "Alpha"},
							map[string]any{"const": "beta", "title": "Beta"},
							map[string]any{"const": "gamma", "title": "Gamma"},
						},
					},
				},
			},
		},
	}
	inlineResult := RenderElicitationRequestCard("req-inline", inlinePayload, FormDrafts{}, "user-1")
	if inlineResult.TextFallback {
		t.Fatalf("RenderElicitationRequestCard(inline) unexpectedly fell back: %s", inlineResult.FallbackReason)
	}
	if got := cardMarkdownContentForTest(inlineResult.Card); !strings.Contains(got, "Configure deployment") || !strings.Contains(got, "请在下方表单中填写") {
		t.Fatalf("inline card body = %q", got)
	}
	form := elicitationFormForTest(t, inlineResult.Card)
	if inputs := formInputsForTest(form); inputs["name"] == nil {
		t.Fatalf("inline form inputs = %+v, want name input", inputs)
	}
	selects := formSelectsForTest(form)
	if selects["enabled"] == nil || selects["mode"] == nil {
		t.Fatalf("inline form selects = %+v, want enabled/mode selects", selects)
	}
	if buttons := formButtonsForTest(form); buttons["elicitation_form_submit"] == nil {
		t.Fatalf("inline form buttons = %+v, want submit", buttons)
	}
	if toggles := elicitationToggleButtonsForTest(form); len(toggles) != 3 {
		t.Fatalf("inline form toggle buttons = %+v, want 3", toggles)
	}

	decisionPayload := ElicitationFormPayload{
		Message: "Enable the feature?",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"enabled": map[string]any{
					"type": "boolean",
				},
			},
		},
	}
	decisionResult := RenderElicitationRequestCard("req-decision", decisionPayload, FormDrafts{}, "user-1")
	if decisionResult.TextFallback {
		t.Fatalf("RenderElicitationRequestCard(decision) unexpectedly fell back: %s", decisionResult.FallbackReason)
	}
	if form := elicitationFormForTestOptional(decisionResult.Card); form != nil {
		t.Fatalf("decision card should not render inline form: %#v", form)
	}
	if got := cardMarkdownContentForTest(decisionResult.Card); !strings.Contains(got, "请直接点击下方按钮完成选择") {
		t.Fatalf("decision card body = %q", got)
	}
	if buttons := formButtonsForTest(decisionResult.Card["body"].(map[string]any)); len(buttons) < 2 {
		t.Fatalf("decision card buttons = %+v, want quick-answer buttons", buttons)
	}

	confirmPayload := ElicitationFormPayload{
		Message: "Allow the MCP server to run the tool?",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
	confirmResult := RenderElicitationRequestCard("req-confirm", confirmPayload, FormDrafts{}, "user-1")
	if confirmResult.TextFallback {
		t.Fatalf("RenderElicitationRequestCard(confirm) unexpectedly fell back: %s", confirmResult.FallbackReason)
	}
	if got := cardMarkdownContentForTest(confirmResult.Card); !strings.Contains(got, "请直接点击下方按钮确认") {
		t.Fatalf("confirm card body = %q", got)
	}
	if buttons := formButtonsForTest(confirmResult.Card["body"].(map[string]any)); len(buttons) < 3 {
		t.Fatalf("confirm card buttons = %+v, want allow/decline/cancel", buttons)
	}

	fallbackPayload := ElicitationFormPayload{
		Message: "Unsupported",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"config": map[string]any{"type": "object"},
			},
		},
	}
	fallbackResult := RenderElicitationRequestCard("req-fallback", fallbackPayload, FormDrafts{}, "user-1")
	if !fallbackResult.TextFallback {
		t.Fatal("expected unsupported schema to fall back to text mode")
	}
	if got := cardMarkdownContentForTest(fallbackResult.Card); !strings.Contains(got, "已回退为文本填写模式") || !strings.Contains(got, "unsupported") {
		t.Fatalf("fallback card body = %q", got)
	}
}

func TestBuildElicitationResponseFromDraftsAndTextParsing(t *testing.T) {
	payload := ElicitationFormPayload{
		Schema: map[string]any{
			"type":     "object",
			"required": []any{"email", "tags"},
			"properties": map[string]any{
				"email": map[string]any{
					"type":   "string",
					"format": "email",
				},
				"count": map[string]any{
					"type":    "integer",
					"minimum": 1.0,
					"maximum": 5.0,
				},
				"enabled": map[string]any{
					"type": "boolean",
				},
				"mode": map[string]any{
					"type": "string",
					"oneOf": []any{
						map[string]any{"const": "fast", "title": "Fast"},
						map[string]any{"const": "safe", "title": "Safe"},
					},
				},
				"tags": map[string]any{
					"type":     "array",
					"minItems": 1.0,
					"maxItems": 2.0,
					"items": map[string]any{
						"enum": []any{"alpha", "beta", "gamma"},
					},
				},
			},
		},
	}

	content, summary, err := BuildElicitationResponseFromDrafts(payload, FormDrafts{
		Values: map[string]string{
			"email":   "bot@example.com",
			"count":   "3",
			"enabled": "true",
			"mode":    "safe",
		},
		Multi: map[string][]string{
			"tags": []string{"alpha", "beta"},
		},
	})
	if err != nil {
		t.Fatalf("BuildElicitationResponseFromDrafts() error = %v", err)
	}
	if content["email"] != "bot@example.com" || content["count"] != int64(3) || content["enabled"] != true || content["mode"] != "safe" {
		t.Fatalf("draft content = %+v", content)
	}
	tags, _ := content["tags"].([]string)
	if len(tags) != 2 || tags[0] != "alpha" || tags[1] != "beta" {
		t.Fatalf("draft tags = %+v, want [alpha beta]", tags)
	}
	if !strings.Contains(summary, "`mode`: safe") || !strings.Contains(summary, "`tags`: alpha, beta") {
		t.Fatalf("draft summary = %q", summary)
	}

	textContent, textSummary, err := ParseElicitationFormResponse("email: bot@example.com\ncount: 2\nenabled: yes\nmode: Fast\ntags: alpha, gamma", payload)
	if err != nil {
		t.Fatalf("ParseElicitationFormResponse() error = %v", err)
	}
	if textContent["mode"] != "fast" || textContent["enabled"] != true {
		t.Fatalf("text content = %+v", textContent)
	}
	if !strings.Contains(textSummary, "`mode`: fast") || !strings.Contains(textSummary, "`tags`: alpha, gamma") {
		t.Fatalf("text summary = %q", textSummary)
	}

	if _, _, err := BuildElicitationResponseFromDrafts(payload, FormDrafts{
		Values: map[string]string{"email": "bad", "count": "7"},
		Multi:  map[string][]string{"tags": []string{"alpha", "beta", "gamma"}},
	}); err == nil {
		t.Fatal("expected invalid draft validation to fail")
	}
}

func elicitationFormForTest(t *testing.T, card map[string]any) map[string]any {
	t.Helper()
	form := elicitationFormForTestOptional(card)
	if form == nil {
		t.Fatalf("elicitation card missing form: %#v", card)
	}
	return form
}

func elicitationFormForTestOptional(card map[string]any) map[string]any {
	for _, elem := range cardElementsForTest(card) {
		if tag, _ := elem["tag"].(string); tag == "form" {
			name, _ := elem["name"].(string)
			if name == "elicitation_form" {
				return elem
			}
		}
	}
	return nil
}

func formInputsForTest(form map[string]any) map[string]map[string]any {
	elements, _ := form["elements"].([]map[string]any)
	inputs := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "input" {
			continue
		}
		name, _ := elem["name"].(string)
		inputs[name] = elem
	}
	return inputs
}

func formSelectsForTest(form map[string]any) map[string]map[string]any {
	elements, _ := form["elements"].([]map[string]any)
	selects := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "select_static" {
			continue
		}
		name, _ := elem["name"].(string)
		selects[name] = elem
	}
	return selects
}

func formButtonsForTest(form map[string]any) map[string]map[string]any {
	var elements []map[string]any
	if raw, ok := form["elements"].([]map[string]any); ok {
		elements = raw
	} else if body, ok := form["elements"].([]any); ok {
		_ = body
	}
	buttons := make(map[string]map[string]any)
	for _, elem := range elements {
		if tag, _ := elem["tag"].(string); tag != "column_set" {
			continue
		}
		columns, _ := elem["columns"].([]map[string]any)
		for _, column := range columns {
			columnElems, _ := column["elements"].([]map[string]any)
			for _, child := range columnElems {
				if tag, _ := child["tag"].(string); tag != "button" {
					continue
				}
				name, _ := child["name"].(string)
				buttons[name] = child
			}
		}
	}
	return buttons
}

func elicitationToggleButtonsForTest(form map[string]any) []map[string]any {
	all := formButtonsForTest(form)
	out := make([]map[string]any, 0, len(all))
	for name, button := range all {
		if strings.HasPrefix(name, "elicitation_toggle_") {
			out = append(out, button)
		}
	}
	return out
}

func cardMarkdownContentForTest(card map[string]any) string {
	var parts []string
	for _, elem := range cardElementsForTest(card) {
		if tag, _ := elem["tag"].(string); tag != "markdown" {
			continue
		}
		content, _ := elem["content"].(string)
		if strings.TrimSpace(content) != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}
