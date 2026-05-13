package pendingforms

import (
	"strings"
	"testing"

	"feidex/internal/feishu"
)

func TestRenderToolUserInputBodyAndParsingHelpers(t *testing.T) {
	payload := ToolUserInputPayload{
		Questions: []ToolUserInputQuestion{
			{
				ID:       "env",
				Question: "Choose environment",
				Options:  []ToolUserInputOption{{Label: "dev"}, {Label: "prod"}},
			},
			{
				ID:       "password",
				Question: "Provide password",
				IsSecret: true,
			},
		},
	}

	body := RenderToolUserInputBody(payload)
	if !strings.Contains(body, "Choose environment (`env`)") || !strings.Contains(body, "可选值: dev, prod") {
		t.Fatalf("RenderToolUserInputBody() missing expected question text:\n%s", body)
	}
	if !strings.Contains(body, "注意: 此答案会按敏感输入处理") || !strings.Contains(body, "单选题会显示下拉选择，多选题会显示可切换按钮") {
		t.Fatalf("RenderToolUserInputBody() missing secret or form hint:\n%s", body)
	}

	if got := ParseStructuredLines("env: prod\npassword: secret"); got["env"] != "prod" || got["password"] != "secret" {
		t.Fatalf("ParseStructuredLines() = %+v, want parsed key-value map", got)
	}
	if got := SplitAnswerParts("dev, prod\nother"); len(got) != 3 || got[2] != "other" {
		t.Fatalf("SplitAnswerParts() = %+v, want split answers", got)
	}
	if got := SummarizeAnswers([]string{"a", "b"}, false); got != "a, b" {
		t.Fatalf("SummarizeAnswers(false) = %q, want joined values", got)
	}
	if got := SummarizeAnswers([]string{"secret"}, true); got != "[redacted]" {
		t.Fatalf("SummarizeAnswers(true) = %q, want redacted", got)
	}
}

func TestParseQuestionAnswersAndToolUserInputResponse(t *testing.T) {
	question := ToolUserInputQuestion{
		ID: "mode",
		Options: []ToolUserInputOption{
			{Label: "Fast", Description: "Prioritize speed"},
			{Label: "Safe", Description: "Prioritize safety"},
		},
	}
	answers, err := ParseQuestionAnswers("fast", question)
	if err != nil {
		t.Fatalf("ParseQuestionAnswers(options) error = %v", err)
	}
	if len(answers) != 1 || answers[0] != "Fast" {
		t.Fatalf("ParseQuestionAnswers(options) = %+v, want canonical label", answers)
	}
	if _, err := ParseQuestionAnswers("fast, safe", question); err == nil {
		t.Fatal("expected single-select question to reject multiple answers")
	}
	multiAnswers, err := ParseQuestionAnswers("fast, safe", ToolUserInputQuestion{
		ID:          "targets",
		Options:     []ToolUserInputOption{{Label: "Fast"}, {Label: "Safe"}},
		MultiSelect: true,
	})
	if err != nil || len(multiAnswers) != 2 {
		t.Fatalf("ParseQuestionAnswers(multi) = %+v, %v", multiAnswers, err)
	}

	otherAnswers, err := ParseQuestionAnswers("custom", ToolUserInputQuestion{
		ID:      "mode",
		IsOther: true,
		Options: []ToolUserInputOption{{Label: "Fast"}},
	})
	if err != nil || len(otherAnswers) != 1 || otherAnswers[0] != "custom" {
		t.Fatalf("ParseQuestionAnswers(other) = %+v, %v", otherAnswers, err)
	}
	if _, err := ParseQuestionAnswers("", ToolUserInputQuestion{ID: "empty"}); err == nil {
		t.Fatal("expected empty answer to fail")
	}
	if _, err := ParseQuestionAnswers("unknown", question); err == nil {
		t.Fatal("expected unsupported option to fail")
	}

	payload := ToolUserInputPayload{
		Questions: []ToolUserInputQuestion{
			{ID: "mode", Options: []ToolUserInputOption{
				{Label: "Fast", Description: "Prioritize speed"},
				{Label: "Safe", Description: "Prioritize safety"},
			}},
			{ID: "note", IsSecret: true},
		},
	}
	result, summary, err := ParseToolUserInputResponse("mode: safe\nnote: hidden", payload)
	if err != nil {
		t.Fatalf("ParseToolUserInputResponse() error = %v", err)
	}
	answersMap, _ := result["answers"].(map[string]any)
	modeEntry, _ := answersMap["mode"].(map[string]any)
	modeAnswers, _ := modeEntry["answers"].([]string)
	if len(modeAnswers) != 1 || modeAnswers[0] != "Safe" {
		t.Fatalf("parsed mode answers = %+v, want Safe", modeAnswers)
	}
	if !strings.Contains(summary, "`mode`: Safe - Prioritize safety") || !strings.Contains(summary, "`note`: [redacted]") {
		t.Fatalf("summary = %q, want visible and redacted lines", summary)
	}

	single, singleSummary, err := ParseToolUserInputResponse("just one answer", ToolUserInputPayload{
		Questions: []ToolUserInputQuestion{{ID: "text"}},
	})
	if err != nil {
		t.Fatalf("single-question ParseToolUserInputResponse() error = %v", err)
	}
	singleMap := single["answers"].(map[string]any)["text"].(map[string]any)
	if got := singleMap["answers"].([]string); len(got) != 1 || got[0] != "just one answer" {
		t.Fatalf("single-question answers = %+v, want raw text", got)
	}
	if !strings.Contains(singleSummary, "`text`: just one answer") {
		t.Fatalf("single-question summary = %q, want raw answer", singleSummary)
	}
}

func TestToolUserInputQuestionMarkdownShowsDetailedSingleSelectOptions(t *testing.T) {
	q := ToolUserInputQuestion{
		ID:       "mode",
		Question: "Choose mode",
		Options: []ToolUserInputOption{
			{Label: "Fast", Description: "Prioritize speed"},
			{Label: "Safe", Description: "Prioritize safety"},
		},
		IsOther: true,
	}

	body := ToolUserInputQuestionMarkdown(q, "Safe", nil, "custom fallback")
	for _, want := range []string{
		"单选题",
		"选项:",
		"1. Fast - Prioritize speed",
		"2. Safe - Prioritize safety",
		"当前选择: Safe - Prioritize safety",
		"其它值: custom fallback",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("ToolUserInputQuestionMarkdown() = %q, missing %q", body, want)
		}
	}
}

func TestRenderToolUserInputFormCardAndFormSelections(t *testing.T) {
	payload := ToolUserInputPayload{
		Questions: []ToolUserInputQuestion{
			{
				ID:       "mode",
				Question: "Choose mode",
				Options: []ToolUserInputOption{
					{Label: "Fast", Description: "Prioritize speed"},
					{Label: "Safe", Description: "Prioritize safety"},
				},
			},
			{
				ID:       "note",
				Question: "Extra note",
				IsSecret: true,
			},
		},
	}

	card := RenderToolUserInputFormCard("req-1", payload, FormDrafts{
		Values: map[string]string{"mode": "Safe"},
	}, "")
	form := toolUserInputFormForTest(t, card)
	inputs := toolUserInputFormInputsForTest(form)
	if len(inputs) != 1 || inputs["note"] == nil {
		t.Fatalf("tool user input form inputs = %+v, want only note input", inputs)
	}
	selects := toolUserInputFormSelectsForTest(form)
	if len(selects) != 1 || selects["mode"] == nil {
		t.Fatalf("tool user input form selects = %+v, want mode select", selects)
	}
	options, _ := selects["mode"]["options"].([]map[string]any)
	if len(options) != 2 {
		t.Fatalf("mode options = %+v, want 2", options)
	}
	firstText, _ := options[0]["text"].(map[string]any)
	if got, _ := firstText["content"].(string); got != "Fast - Prioritize speed" {
		t.Fatalf("first option text = %q, want full label + description", got)
	}
	if got, _ := selects["mode"]["initial_option"].(string); got != "Safe" {
		t.Fatalf("mode initial_option = %q, want Safe", got)
	}
	buttons := toolUserInputFormButtonsForTest(form)
	if submit := buttons["user_input_submit"]; submit == nil || submit["form_action_type"] != "submit" {
		t.Fatalf("submit button = %+v, want form submit", submit)
	}
	if cancel := buttons["user_input_cancel"]; cancel == nil {
		t.Fatalf("cancel button missing from form: %+v", buttons)
	}

	drafts := ToolUserInputDraftsFromCardAction(payload, &feishu.CardAction{
		ActionValue: map[string]any{
			"multi_drafts": map[string]any{"extra": []any{"A", "B"}},
		},
		FormValue: map[string]any{
			"mode": "Fast",
			"note": "hidden",
		},
	})
	selections := ToolUserInputSelectionsFromDrafts(payload, drafts)
	if selections["mode"] != "Fast" || selections["note"] != "hidden" {
		t.Fatalf("ToolUserInputSelectionsFromDrafts() = %+v", selections)
	}

	multiPayload := ToolUserInputPayload{
		Questions: []ToolUserInputQuestion{
			{
				ID:          "targets",
				Question:    "Pick targets",
				Options:     []ToolUserInputOption{{Label: "A"}, {Label: "B"}, {Label: "C"}},
				MultiSelect: true,
				IsOther:     true,
			},
		},
	}
	multiCard := RenderToolUserInputFormCard("req-2", multiPayload, FormDrafts{
		Values: map[string]string{ToolUserInputOtherFieldName(multiPayload.Questions[0]): "custom"},
		Multi:  map[string][]string{"targets": {"A", "C"}},
	}, "")
	multiForm := toolUserInputFormForTest(t, multiCard)
	multiButtons := toolUserInputFormButtonsForTest(multiForm)
	if multiButtons["user_input_submit"] == nil {
		t.Fatalf("multi-select submit button missing: %+v", multiButtons)
	}
	if toggle := toolUserInputToggleButtonsForTest(multiForm); len(toggle) != 3 {
		t.Fatalf("multi-select toggle buttons = %+v, want 3", toggle)
	}
	if otherInput := toolUserInputFormInputsForTest(multiForm)[ToolUserInputOtherFieldName(multiPayload.Questions[0])]; otherInput == nil {
		t.Fatalf("multi-select other input missing: %+v", toolUserInputFormInputsForTest(multiForm))
	}
	multiDrafts := ToolUserInputDraftsFromCardAction(multiPayload, &feishu.CardAction{
		ActionValue: map[string]any{
			"multi_drafts": map[string]any{"targets": []any{"A", "C"}},
		},
		FormValue: map[string]any{
			ToolUserInputOtherFieldName(multiPayload.Questions[0]): "custom",
		},
	})
	multiSelections := ToolUserInputSelectionsFromDrafts(multiPayload, multiDrafts)
	if multiSelections["targets"] != "A, C, custom" {
		t.Fatalf("ToolUserInputSelectionsFromDrafts(multi) = %+v", multiSelections)
	}
}

func TestRenderAndParseElicitationForms(t *testing.T) {
	payload := ElicitationFormPayload{
		Message: "Fill in the form",
		Schema: map[string]any{
			"required": []any{"name", "enabled"},
			"properties": map[string]any{
				"name": map[string]any{
					"title":       "Project Name",
					"description": "Name shown to users",
					"type":        "string",
				},
				"enabled": map[string]any{
					"type": "boolean",
				},
				"choice": map[string]any{
					"enum":      []any{"fast", "safe"},
					"enumNames": []any{"Fast", "Safe"},
				},
			},
		},
	}

	body := RenderElicitationFormBody(payload)
	if !strings.Contains(body, "Fill in the form") || !strings.Contains(body, "Project Name *") {
		t.Fatalf("RenderElicitationFormBody() missing header or required marker:\n%s", body)
	}
	if !strings.Contains(body, "可选值: Fast, Safe") || !strings.Contains(body, "field_name: value") {
		t.Fatalf("RenderElicitationFormBody() missing option labels or hint:\n%s", body)
	}

	content, summary, err := ParseElicitationFormResponse("name: Feidex\nenabled: yes\nchoice: Safe", payload)
	if err != nil {
		t.Fatalf("ParseElicitationFormResponse() error = %v", err)
	}
	if content["name"] != "Feidex" || content["enabled"] != true || content["choice"] != "safe" {
		t.Fatalf("parsed elicitation content = %+v, want normalized values", content)
	}
	if !strings.Contains(summary, "`enabled`: true") || !strings.Contains(summary, "`choice`: safe") {
		t.Fatalf("elicitation summary = %q, want field summaries", summary)
	}

	if _, _, err := ParseElicitationFormResponse("enabled: yes", payload); err == nil {
		t.Fatal("expected missing required field to fail")
	}

	singleContent, singleSummary, err := ParseElicitationFormResponse("42", ElicitationFormPayload{
		Schema: map[string]any{
			"properties": map[string]any{
				"count": map[string]any{"type": "integer"},
			},
		},
	})
	if err != nil {
		t.Fatalf("single-field ParseElicitationFormResponse() error = %v", err)
	}
	if singleContent["count"] != int64(42) || !strings.Contains(singleSummary, "`count`: 42") {
		t.Fatalf("single-field parse result = %+v, summary=%q", singleContent, singleSummary)
	}
}

func TestElicitationFieldHelpers(t *testing.T) {
	boolValue, boolSummary, err := ParseElicitationFieldValue("yes", map[string]any{"type": "boolean"})
	if err != nil || boolValue != true || boolSummary != "true" {
		t.Fatalf("ParseElicitationFieldValue(boolean) = %v, %q, %v", boolValue, boolSummary, err)
	}
	numberValue, _, err := ParseElicitationFieldValue("3.14", map[string]any{"type": "number"})
	if err != nil || numberValue != 3.14 {
		t.Fatalf("ParseElicitationFieldValue(number) = %v, %v", numberValue, err)
	}
	integerValue, _, err := ParseElicitationFieldValue("7", map[string]any{"type": "integer"})
	if err != nil || integerValue != int64(7) {
		t.Fatalf("ParseElicitationFieldValue(integer) = %v, %v", integerValue, err)
	}
	arrayValue, arraySummary, err := ParseElicitationFieldValue("a, b", map[string]any{"type": "array"})
	if err != nil || len(arrayValue.([]string)) != 2 || arraySummary != "a, b" {
		t.Fatalf("ParseElicitationFieldValue(array) = %v, %q, %v", arrayValue, arraySummary, err)
	}
	enumValue, enumSummary, err := ParseElicitationFieldValue("Fast", map[string]any{
		"enum":      []any{"fast", "safe"},
		"enumNames": []any{"Fast", "Safe"},
	})
	if err != nil || enumValue != "fast" || enumSummary != "fast" {
		t.Fatalf("ParseElicitationFieldValue(enum) = %v, %q, %v", enumValue, enumSummary, err)
	}
	stringValue, stringSummary, err := ParseElicitationFieldValue("hello", map[string]any{"type": "string"})
	if err != nil || stringValue != "hello" || stringSummary != "hello" {
		t.Fatalf("ParseElicitationFieldValue(string) = %v, %q, %v", stringValue, stringSummary, err)
	}
	if _, _, err := ParseElicitationFieldValue("bad", map[string]any{"type": "boolean"}); err == nil {
		t.Fatal("expected invalid boolean to fail")
	}

	field := map[string]any{
		"title":       "Environment",
		"description": "Choose target",
		"type":        "string",
		"enum":        []any{"dev", "prod"},
		"enumNames":   []any{"Development", "Production"},
	}
	if got := StringField(field, "title"); got != "Environment" {
		t.Fatalf("StringField(title) = %q, want Environment", got)
	}
	if got := FieldType(field); got != "string" {
		t.Fatalf("FieldType() = %q, want string", got)
	}
	if got := DisplayFieldTitle("env", field); got != "Environment" {
		t.Fatalf("DisplayFieldTitle() = %q, want title", got)
	}
	if got := SchemaOptionLabels(field); len(got) != 2 || got[0] != "Development" || got[1] != "Production" {
		t.Fatalf("SchemaOptionLabels(enum) = %+v, want enum names", got)
	}
	if got, err := MatchSchemaOption("Production", field); err != nil || got != "prod" {
		t.Fatalf("MatchSchemaOption(enum) = %q, %v", got, err)
	}
	if got, err := MatchSchemaOption("beta", map[string]any{
		"oneOf": []any{
			map[string]any{"const": "alpha", "title": "Alpha"},
			map[string]any{"const": "beta", "title": "Beta"},
		},
	}); err != nil || got != "beta" {
		t.Fatalf("MatchSchemaOption(oneOf) = %q, %v", got, err)
	}
	if got := SchemaOptionLabels(map[string]any{
		"oneOf": []any{
			map[string]any{"const": "alpha", "title": "Alpha"},
			map[string]any{"const": "beta"},
		},
	}); len(got) != 2 || got[0] != "Alpha" || got[1] != "beta" {
		t.Fatalf("SchemaOptionLabels(oneOf) = %+v, want labels and fallback const", got)
	}
	if got, err := MatchSchemaOption("anything", map[string]any{"items": map[string]any{"anyOf": []any{}}}); err != nil || got != "anything" {
		t.Fatalf("MatchSchemaOption(items) = %q, %v", got, err)
	}
	if got := SchemaOptionLabels(map[string]any{
		"items": map[string]any{
			"anyOf": []any{
				map[string]any{"const": "a", "title": "Option A"},
				map[string]any{"const": "b"},
			},
		},
	}); len(got) != 2 || got[0] != "Option A" || got[1] != "b" {
		t.Fatalf("SchemaOptionLabels(items.anyOf) = %+v, want labels and fallback const", got)
	}
	if _, err := MatchSchemaOption("missing", field); err == nil {
		t.Fatal("expected unsupported option to fail")
	}

	required := RequiredSet(map[string]any{"required": []any{"name", "enabled"}})
	if !required["name"] || !required["enabled"] {
		t.Fatalf("RequiredSet() = %+v, want required flags", required)
	}
	keys := SortedMapKeys(map[string]any{"b": 1, "a": 2})
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("SortedMapKeys() = %+v, want sorted keys", keys)
	}
	if got := RequiredMarker(true); got != " *" {
		t.Fatalf("RequiredMarker(true) = %q, want marker", got)
	}
	if got := RequiredMarker(false); got != "" {
		t.Fatalf("RequiredMarker(false) = %q, want empty string", got)
	}
	if got, err := ParseBool("是"); err != nil || !got {
		t.Fatalf("ParseBool(是) = %v, %v", got, err)
	}
	if got, err := ParseBool("0"); err != nil || got {
		t.Fatalf("ParseBool(0) = %v, %v", got, err)
	}
	if _, err := ParseBool("maybe"); err == nil {
		t.Fatal("expected ParseBool(maybe) to fail")
	}
}

func toolUserInputFormForTest(t *testing.T, card map[string]any) map[string]any {
	t.Helper()
	for _, elem := range cardElementsForTest(card) {
		if tag, _ := elem["tag"].(string); tag == "form" {
			name, _ := elem["name"].(string)
			if name == "tool_user_input_form" {
				return elem
			}
		}
	}
	t.Fatalf("tool user input card missing form: %#v", card)
	return nil
}

func toolUserInputFormInputsForTest(form map[string]any) map[string]map[string]any {
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

func toolUserInputFormSelectsForTest(form map[string]any) map[string]map[string]any {
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

func toolUserInputFormButtonsForTest(form map[string]any) map[string]map[string]any {
	elements, _ := form["elements"].([]map[string]any)
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

func toolUserInputToggleButtonsForTest(form map[string]any) []map[string]any {
	all := toolUserInputFormButtonsForTest(form)
	out := make([]map[string]any, 0, len(all))
	for name, button := range all {
		if strings.HasPrefix(name, "toggle_") {
			out = append(out, button)
		}
	}
	return out
}

func cardElementsForTest(card map[string]any) []map[string]any {
	body, _ := card["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	return elements
}
