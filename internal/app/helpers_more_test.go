package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type attachmentDownloadFeishuStub struct {
	*fakeFeishuClient
	downloadPath string
	messageIDs   []string
}

func (s *attachmentDownloadFeishuStub) DownloadMessageResource(_ context.Context, messageID string, _ feishu.Attachment, _ string) (string, string, error) {
	s.messageIDs = append(s.messageIDs, messageID)
	return s.downloadPath, filepath.Base(s.downloadPath), nil
}

func TestPendingTextRequestPrefersLatestMatchingRequest(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := &App{store: store}

	for _, req := range []*state.PendingRequest{
		{ID: "old", Kind: "workspace_new", SessionKey: "sess", OwnerUserID: "u-1", Status: "pending", CreatedAt: 1},
		{ID: "wrong-user", Kind: "workspace_new", SessionKey: "sess", OwnerUserID: "u-2", Status: "pending", CreatedAt: 3},
		{ID: "wrong-kind", Kind: "permissions", SessionKey: "sess", OwnerUserID: "u-1", Status: "pending", CreatedAt: 4},
		{ID: "latest", Kind: "tool_request_user_input_form", SessionKey: "sess", OwnerUserID: "u-1", Status: "pending", CreatedAt: 5},
	} {
		if err := a.store.UpsertPending(req); err != nil {
			t.Fatalf("UpsertPending(%s): %v", req.ID, err)
		}
	}

	got := a.pendingTextRequest("sess", "u-1")
	if got == nil || got.ID != "latest" {
		t.Fatalf("pendingTextRequest() = %+v, want latest matching request", got)
	}
	if got := a.pendingTextRequest("sess", "missing"); got != nil {
		t.Fatalf("pendingTextRequest(non-owner) = %+v, want nil", got)
	}
}

func TestShouldRedactInboundTextForSensitiveRequests(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	a := &App{store: store}

	if a.shouldRedactInboundText("sess", "u-1") {
		t.Fatal("expected no redaction without pending request")
	}

	req := &state.PendingRequest{
		ID:          "secret-form",
		Kind:        "tool_request_user_input_form",
		SessionKey:  "sess",
		OwnerUserID: "u-1",
		Status:      "pending",
		PayloadJSON: mustJSON(toolUserInputPayload{
			Questions: []toolUserInputQuestion{{ID: "password", IsSecret: true}},
		}),
	}
	if err := a.store.UpsertPending(req); err != nil {
		t.Fatalf("UpsertPending(secret form): %v", err)
	}
	if !a.shouldRedactInboundText("sess", "u-1") {
		t.Fatal("expected secret user-input request to be redacted")
	}

	req = &state.PendingRequest{
		ID:          "elicitation",
		Kind:        "mcp_elicitation_form",
		SessionKey:  "sess2",
		OwnerUserID: "u-1",
		Status:      "pending",
	}
	if err := a.store.UpsertPending(req); err != nil {
		t.Fatalf("UpsertPending(elicitation): %v", err)
	}
	if !a.shouldRedactInboundText("sess2", "u-1") {
		t.Fatal("expected elicitation request to be redacted")
	}
}

func TestRenderToolUserInputBodyAndParsingHelpers(t *testing.T) {
	payload := toolUserInputPayload{
		Questions: []toolUserInputQuestion{
			{
				ID:       "env",
				Question: "Choose environment",
				Options:  []toolUserInputOption{{Label: "dev"}, {Label: "prod"}},
			},
			{
				ID:       "password",
				Question: "Provide password",
				IsSecret: true,
			},
		},
	}

	body := renderToolUserInputBody(payload)
	if !strings.Contains(body, "Choose environment (`env`)") || !strings.Contains(body, "可选值: dev, prod") {
		t.Fatalf("renderToolUserInputBody() missing expected question text:\n%s", body)
	}
	if !strings.Contains(body, "注意: 此答案会按敏感输入处理") || !strings.Contains(body, "单选题会显示下拉选择，多选题会显示可切换按钮") {
		t.Fatalf("renderToolUserInputBody() missing secret or form hint:\n%s", body)
	}

	if got := parseStructuredLines("env: prod\npassword: secret"); got["env"] != "prod" || got["password"] != "secret" {
		t.Fatalf("parseStructuredLines() = %+v, want parsed key-value map", got)
	}
	if got := splitAnswerParts("dev, prod\nother"); len(got) != 3 || got[2] != "other" {
		t.Fatalf("splitAnswerParts() = %+v, want split answers", got)
	}
	if got := summarizeAnswers([]string{"a", "b"}, false); got != "a, b" {
		t.Fatalf("summarizeAnswers(false) = %q, want joined values", got)
	}
	if got := summarizeAnswers([]string{"secret"}, true); got != "[redacted]" {
		t.Fatalf("summarizeAnswers(true) = %q, want redacted", got)
	}
}

func TestParseQuestionAnswersAndToolUserInputResponse(t *testing.T) {
	question := toolUserInputQuestion{
		ID:      "mode",
		Options: []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}},
	}
	answers, err := parseQuestionAnswers("fast", question)
	if err != nil {
		t.Fatalf("parseQuestionAnswers(options) error = %v", err)
	}
	if len(answers) != 1 || answers[0] != "Fast" {
		t.Fatalf("parseQuestionAnswers(options) = %+v, want canonical label", answers)
	}
	if _, err := parseQuestionAnswers("fast, safe", question); err == nil {
		t.Fatal("expected single-select question to reject multiple answers")
	}
	multiAnswers, err := parseQuestionAnswers("fast, safe", toolUserInputQuestion{
		ID:          "targets",
		Options:     []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}},
		MultiSelect: true,
	})
	if err != nil || len(multiAnswers) != 2 {
		t.Fatalf("parseQuestionAnswers(multi) = %+v, %v", multiAnswers, err)
	}

	otherAnswers, err := parseQuestionAnswers("custom", toolUserInputQuestion{
		ID:      "mode",
		IsOther: true,
		Options: []toolUserInputOption{{Label: "Fast"}},
	})
	if err != nil || len(otherAnswers) != 1 || otherAnswers[0] != "custom" {
		t.Fatalf("parseQuestionAnswers(other) = %+v, %v", otherAnswers, err)
	}
	if _, err := parseQuestionAnswers("", toolUserInputQuestion{ID: "empty"}); err == nil {
		t.Fatal("expected empty answer to fail")
	}
	if _, err := parseQuestionAnswers("unknown", question); err == nil {
		t.Fatal("expected unsupported option to fail")
	}

	payload := toolUserInputPayload{
		Questions: []toolUserInputQuestion{
			{ID: "mode", Options: []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}}},
			{ID: "note", IsSecret: true},
		},
	}
	result, summary, err := parseToolUserInputResponse("mode: safe\nnote: hidden", payload)
	if err != nil {
		t.Fatalf("parseToolUserInputResponse() error = %v", err)
	}
	answersMap, _ := result["answers"].(map[string]any)
	modeEntry, _ := answersMap["mode"].(map[string]any)
	modeAnswers, _ := modeEntry["answers"].([]string)
	if len(modeAnswers) != 1 || modeAnswers[0] != "Safe" {
		t.Fatalf("parsed mode answers = %+v, want Safe", modeAnswers)
	}
	if !strings.Contains(summary, "`mode`: Safe") || !strings.Contains(summary, "`note`: [redacted]") {
		t.Fatalf("summary = %q, want visible and redacted lines", summary)
	}

	single, singleSummary, err := parseToolUserInputResponse("just one answer", toolUserInputPayload{
		Questions: []toolUserInputQuestion{{ID: "text"}},
	})
	if err != nil {
		t.Fatalf("single-question parseToolUserInputResponse() error = %v", err)
	}
	singleMap := single["answers"].(map[string]any)["text"].(map[string]any)
	if got := singleMap["answers"].([]string); len(got) != 1 || got[0] != "just one answer" {
		t.Fatalf("single-question answers = %+v, want raw text", got)
	}
	if !strings.Contains(singleSummary, "`text`: just one answer") {
		t.Fatalf("single-question summary = %q, want raw answer", singleSummary)
	}
}

func TestRenderToolUserInputFormCardAndFormSelections(t *testing.T) {
	payload := toolUserInputPayload{
		Questions: []toolUserInputQuestion{
			{
				ID:       "mode",
				Question: "Choose mode",
				Options:  []toolUserInputOption{{Label: "Fast"}, {Label: "Safe"}},
			},
			{
				ID:       "note",
				Question: "Extra note",
				IsSecret: true,
			},
		},
	}

	card := renderToolUserInputFormCard("req-1", payload, toolUserInputFormDrafts{
		Values: map[string]string{"mode": "Safe"},
	}, "")
	form := toolUserInputFormForTest(t, card)
	inputs := toolUserInputFormInputsForTest(t, form)
	if len(inputs) != 1 || inputs["note"] == nil {
		t.Fatalf("tool user input form inputs = %+v, want only note input", inputs)
	}
	selects := toolUserInputFormSelectsForTest(t, form)
	if len(selects) != 1 || selects["mode"] == nil {
		t.Fatalf("tool user input form selects = %+v, want mode select", selects)
	}
	if got, _ := selects["mode"]["initial_option"].(string); got != "Safe" {
		t.Fatalf("mode initial_option = %q, want Safe", got)
	}
	buttons := toolUserInputFormButtonsForTest(t, form)
	if submit := buttons["user_input_submit"]; submit == nil || submit["form_action_type"] != "submit" {
		t.Fatalf("submit button = %+v, want form submit", submit)
	}
	if cancel := buttons["user_input_cancel"]; cancel == nil {
		t.Fatalf("cancel button missing from form: %+v", buttons)
	}

	drafts := toolUserInputDraftsFromCardAction(payload, &feishu.CardAction{
		ActionValue: map[string]any{
			"multi_drafts": map[string]any{"extra": []any{"A", "B"}},
		},
		FormValue: map[string]any{
			"mode": "Fast",
			"note": "hidden",
		},
	})
	selections := toolUserInputSelectionsFromDrafts(payload, drafts)
	if selections["mode"] != "Fast" || selections["note"] != "hidden" {
		t.Fatalf("toolUserInputSelectionsFromDrafts() = %+v", selections)
	}

	multiPayload := toolUserInputPayload{
		Questions: []toolUserInputQuestion{
			{
				ID:          "targets",
				Question:    "Pick targets",
				Options:     []toolUserInputOption{{Label: "A"}, {Label: "B"}, {Label: "C"}},
				MultiSelect: true,
				IsOther:     true,
			},
		},
	}
	multiCard := renderToolUserInputFormCard("req-2", multiPayload, toolUserInputFormDrafts{
		Values: map[string]string{toolUserInputOtherFieldName(multiPayload.Questions[0]): "custom"},
		Multi:  map[string][]string{"targets": []string{"A", "C"}},
	}, "")
	multiForm := toolUserInputFormForTest(t, multiCard)
	multiButtons := toolUserInputFormButtonsForTest(t, multiForm)
	if multiButtons["user_input_submit"] == nil {
		t.Fatalf("multi-select submit button missing: %+v", multiButtons)
	}
	if toggle := toolUserInputToggleButtonsForTest(t, multiForm); len(toggle) != 3 {
		t.Fatalf("multi-select toggle buttons = %+v, want 3", toggle)
	}
	if otherInput := toolUserInputFormInputsForTest(t, multiForm)[toolUserInputOtherFieldName(multiPayload.Questions[0])]; otherInput == nil {
		t.Fatalf("multi-select other input missing: %+v", toolUserInputFormInputsForTest(t, multiForm))
	}
	multiDrafts := toolUserInputDraftsFromCardAction(multiPayload, &feishu.CardAction{
		ActionValue: map[string]any{
			"multi_drafts": map[string]any{"targets": []any{"A", "C"}},
		},
		FormValue: map[string]any{
			toolUserInputOtherFieldName(multiPayload.Questions[0]): "custom",
		},
	})
	multiSelections := toolUserInputSelectionsFromDrafts(multiPayload, multiDrafts)
	if multiSelections["targets"] != "A, C, custom" {
		t.Fatalf("toolUserInputSelectionsFromDrafts(multi) = %+v", multiSelections)
	}
}

func toolUserInputFormForTest(t *testing.T, card map[string]any) map[string]any {
	t.Helper()
	for _, elem := range cardElements(card) {
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

func toolUserInputFormInputsForTest(t *testing.T, form map[string]any) map[string]map[string]any {
	t.Helper()
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

func toolUserInputFormSelectsForTest(t *testing.T, form map[string]any) map[string]map[string]any {
	t.Helper()
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

func toolUserInputFormButtonsForTest(t *testing.T, form map[string]any) map[string]map[string]any {
	t.Helper()
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

func toolUserInputToggleButtonsForTest(t *testing.T, form map[string]any) []map[string]any {
	t.Helper()
	all := toolUserInputFormButtonsForTest(t, form)
	out := make([]map[string]any, 0, len(all))
	for name, button := range all {
		if !strings.HasPrefix(name, "toggle_") {
			continue
		}
		out = append(out, button)
	}
	return out
}

func TestRenderAndParseElicitationForms(t *testing.T) {
	payload := elicitationFormPayload{
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

	body := renderElicitationFormBody(payload)
	if !strings.Contains(body, "Fill in the form") || !strings.Contains(body, "Project Name *") {
		t.Fatalf("renderElicitationFormBody() missing header or required marker:\n%s", body)
	}
	if !strings.Contains(body, "可选值: Fast, Safe") || !strings.Contains(body, "field_name: value") {
		t.Fatalf("renderElicitationFormBody() missing option labels or hint:\n%s", body)
	}

	content, summary, err := parseElicitationFormResponse("name: Feidex\nenabled: yes\nchoice: Safe", payload)
	if err != nil {
		t.Fatalf("parseElicitationFormResponse() error = %v", err)
	}
	if content["name"] != "Feidex" || content["enabled"] != true || content["choice"] != "safe" {
		t.Fatalf("parsed elicitation content = %+v, want normalized values", content)
	}
	if !strings.Contains(summary, "`enabled`: true") || !strings.Contains(summary, "`choice`: safe") {
		t.Fatalf("elicitation summary = %q, want field summaries", summary)
	}

	if _, _, err := parseElicitationFormResponse("enabled: yes", payload); err == nil {
		t.Fatal("expected missing required field to fail")
	}

	singleContent, singleSummary, err := parseElicitationFormResponse("42", elicitationFormPayload{
		Schema: map[string]any{
			"properties": map[string]any{
				"count": map[string]any{"type": "integer"},
			},
		},
	})
	if err != nil {
		t.Fatalf("single-field parseElicitationFormResponse() error = %v", err)
	}
	if singleContent["count"] != int64(42) || !strings.Contains(singleSummary, "`count`: 42") {
		t.Fatalf("single-field parse result = %+v, summary=%q", singleContent, singleSummary)
	}
}

func TestElicitationFieldHelpers(t *testing.T) {
	boolValue, boolSummary, err := parseElicitationFieldValue("yes", map[string]any{"type": "boolean"})
	if err != nil || boolValue != true || boolSummary != "true" {
		t.Fatalf("parseElicitationFieldValue(boolean) = %v, %q, %v", boolValue, boolSummary, err)
	}
	numberValue, _, err := parseElicitationFieldValue("3.14", map[string]any{"type": "number"})
	if err != nil || numberValue != 3.14 {
		t.Fatalf("parseElicitationFieldValue(number) = %v, %v", numberValue, err)
	}
	integerValue, _, err := parseElicitationFieldValue("7", map[string]any{"type": "integer"})
	if err != nil || integerValue != int64(7) {
		t.Fatalf("parseElicitationFieldValue(integer) = %v, %v", integerValue, err)
	}
	arrayValue, arraySummary, err := parseElicitationFieldValue("a, b", map[string]any{"type": "array"})
	if err != nil || len(arrayValue.([]string)) != 2 || arraySummary != "a, b" {
		t.Fatalf("parseElicitationFieldValue(array) = %v, %q, %v", arrayValue, arraySummary, err)
	}
	enumValue, enumSummary, err := parseElicitationFieldValue("Fast", map[string]any{
		"enum":      []any{"fast", "safe"},
		"enumNames": []any{"Fast", "Safe"},
	})
	if err != nil || enumValue != "fast" || enumSummary != "fast" {
		t.Fatalf("parseElicitationFieldValue(enum) = %v, %q, %v", enumValue, enumSummary, err)
	}
	stringValue, stringSummary, err := parseElicitationFieldValue("hello", map[string]any{"type": "string"})
	if err != nil || stringValue != "hello" || stringSummary != "hello" {
		t.Fatalf("parseElicitationFieldValue(string) = %v, %q, %v", stringValue, stringSummary, err)
	}
	if _, _, err := parseElicitationFieldValue("bad", map[string]any{"type": "boolean"}); err == nil {
		t.Fatal("expected invalid boolean to fail")
	}

	field := map[string]any{
		"title":       "Environment",
		"description": "Choose target",
		"type":        "string",
		"enum":        []any{"dev", "prod"},
		"enumNames":   []any{"Development", "Production"},
	}
	if got := stringField(field, "title"); got != "Environment" {
		t.Fatalf("stringField(title) = %q, want Environment", got)
	}
	if got := fieldType(field); got != "string" {
		t.Fatalf("fieldType() = %q, want string", got)
	}
	if got := displayFieldTitle("env", field); got != "Environment" {
		t.Fatalf("displayFieldTitle() = %q, want title", got)
	}
	if got := schemaOptionLabels(field); len(got) != 2 || got[0] != "Development" || got[1] != "Production" {
		t.Fatalf("schemaOptionLabels(enum) = %+v, want enum names", got)
	}
	if got, err := matchSchemaOption("Production", field); err != nil || got != "prod" {
		t.Fatalf("matchSchemaOption(enum) = %q, %v", got, err)
	}
	if got, err := matchSchemaOption("beta", map[string]any{
		"oneOf": []any{
			map[string]any{"const": "alpha", "title": "Alpha"},
			map[string]any{"const": "beta", "title": "Beta"},
		},
	}); err != nil || got != "beta" {
		t.Fatalf("matchSchemaOption(oneOf) = %q, %v", got, err)
	}
	if got := schemaOptionLabels(map[string]any{
		"oneOf": []any{
			map[string]any{"const": "alpha", "title": "Alpha"},
			map[string]any{"const": "beta"},
		},
	}); len(got) != 2 || got[0] != "Alpha" || got[1] != "beta" {
		t.Fatalf("schemaOptionLabels(oneOf) = %+v, want labels and fallback const", got)
	}
	if got, err := matchSchemaOption("anything", map[string]any{"items": map[string]any{"anyOf": []any{}}}); err != nil || got != "anything" {
		t.Fatalf("matchSchemaOption(items) = %q, %v", got, err)
	}
	if got := schemaOptionLabels(map[string]any{
		"items": map[string]any{
			"anyOf": []any{
				map[string]any{"const": "a", "title": "Option A"},
				map[string]any{"const": "b"},
			},
		},
	}); len(got) != 2 || got[0] != "Option A" || got[1] != "b" {
		t.Fatalf("schemaOptionLabels(items.anyOf) = %+v, want labels and fallback const", got)
	}
	if _, err := matchSchemaOption("missing", field); err == nil {
		t.Fatal("expected unsupported option to fail")
	}

	required := requiredSet(map[string]any{"required": []any{"name", "enabled"}})
	if !required["name"] || !required["enabled"] {
		t.Fatalf("requiredSet() = %+v, want required flags", required)
	}
	keys := sortedMapKeys(map[string]any{"b": 1, "a": 2})
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("sortedMapKeys() = %+v, want sorted keys", keys)
	}
	if got := requiredMarker(true); got != " *" {
		t.Fatalf("requiredMarker(true) = %q, want marker", got)
	}
	if got := requiredMarker(false); got != "" {
		t.Fatalf("requiredMarker(false) = %q, want empty string", got)
	}
	if got, err := parseBool("是"); err != nil || !got {
		t.Fatalf("parseBool(是) = %v, %v", got, err)
	}
	if got, err := parseBool("0"); err != nil || got {
		t.Fatalf("parseBool(0) = %v, %v", got, err)
	}
	if _, err := parseBool("maybe"); err == nil {
		t.Fatal("expected parseBool(maybe) to fail")
	}
}

func TestAttachmentHelpers(t *testing.T) {
	workspace := t.TempDir()
	linked := filepath.Join(workspace, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(linked), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(linked, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile(linked) error = %v", err)
	}
	repaired := filepath.Join(workspace+".md", "docs", "guide")

	dir := sessionAttachmentDir(workspace, "session", "message")
	if !strings.Contains(dir, attachmentsDirName) {
		t.Fatalf("sessionAttachmentDir() = %q, want attachments dir", dir)
	}
	if len(shortHash("value")) != 24 {
		t.Fatalf("shortHash() length = %d, want 24", len(shortHash("value")))
	}

	cfg := config.Default()
	cfg.Workspaces[0].Cwd = workspace
	downloadPath := filepath.Join(t.TempDir(), "forwarded.png")
	if err := os.WriteFile(downloadPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile(downloadPath) error = %v", err)
	}
	stub := &attachmentDownloadFeishuStub{
		fakeFeishuClient: &fakeFeishuClient{},
		downloadPath:     downloadPath,
	}
	a := &App{cfg: cfg, feishu: stub}
	attachments, err := a.resolveInboundAttachments(&feishu.InboundMessage{
		MessageID: "root-message",
		Attachments: []feishu.Attachment{{
			Kind:            "image",
			ResourceKey:     "img-forwarded",
			SourceMessageID: "forwarded-message",
		}},
	}, cfg.Workspaces[0].ID, "sess-1")
	if err != nil {
		t.Fatalf("resolveInboundAttachments() error = %v", err)
	}
	if len(stub.messageIDs) != 1 || stub.messageIDs[0] != "forwarded-message" {
		t.Fatalf("resolveInboundAttachments() download message ids = %+v, want forwarded source", stub.messageIDs)
	}
	if len(attachments) != 1 || attachments[0].LocalPath != downloadPath {
		t.Fatalf("resolveInboundAttachments() attachments = %+v, want downloaded file", attachments)
	}

	sub := &state.Submission{
		InputText: "hello",
		Skills: []state.SubmissionSkill{
			{Name: "openai-docs", Path: "/skills/openai-docs"},
		},
		Attachments: []state.SubmissionAttachment{
			{Kind: "image", LocalPath: "/tmp/image.png"},
			{Kind: "file", LocalPath: "/tmp/doc.txt"},
			{Kind: "audio", LocalPath: "/tmp/audio.wav"},
		},
	}
	inputs := buildTurnInputs(sub)
	if len(inputs) != 5 || inputs[0]["type"] != "skill" || inputs[1]["type"] != "text" || inputs[2]["type"] != "localImage" {
		t.Fatalf("buildTurnInputs() = %+v, want skill + text + localImage + prompts", inputs)
	}
	if inputs[0]["name"] != "openai-docs" || inputs[0]["path"] != "/skills/openai-docs" {
		t.Fatalf("buildTurnInputs() skill item = %+v, want name/path", inputs[0])
	}
	if got := attachmentPrompt(state.SubmissionAttachment{Kind: "audio", LocalPath: "/tmp/a.wav"}); !strings.Contains(got, "audio file") {
		t.Fatalf("attachmentPrompt(audio) = %q, want audio text", got)
	}
	if got := attachmentPrompt(state.SubmissionAttachment{}); got != "" {
		t.Fatalf("attachmentPrompt(empty) = %q, want empty", got)
	}

	preview := submissionInputPreview(&state.Submission{
		InputText: "Question",
		Skills: []state.SubmissionSkill{
			{Name: "openai-docs", Path: "/skills/openai-docs"},
		},
		Attachments: []state.SubmissionAttachment{
			{Kind: "image", Name: "pic.png"},
			{Kind: "file", LocalPath: "/tmp/report.pdf"},
		},
	})
	if !strings.Contains(preview, "[skill] openai-docs") || !strings.Contains(preview, "Question") || !strings.Contains(preview, "[图片] pic.png") || !strings.Contains(preview, "[文件] report.pdf") {
		t.Fatalf("submissionInputPreview() = %q, want skill, text and attachment previews", preview)
	}
	if got := submissionInputPreview(&state.Submission{}); got != "-" {
		t.Fatalf("submissionInputPreview(empty) = %q, want -", got)
	}

	if path, ok := normalizeReferencedPath("./docs/guide.md:12", workspace); !ok || path != linked {
		t.Fatalf("normalizeReferencedPath(relative) = %q, %v, want %q", path, ok, linked)
	}
	if path, ok := normalizeReferencedPath(repaired, workspace); !ok || path != linked {
		t.Fatalf("normalizeReferencedPath(repaired) = %q, %v, want %q", path, ok, linked)
	}
	if _, ok := normalizeReferencedPath("../outside.txt", workspace); ok {
		t.Fatal("expected outside workspace path to be rejected")
	}

	sanitized := sanitizeLocalMarkdownLinks(
		"See [Guide](./docs/guide.md:12) and [.mdguide](missing)",
		workspace,
	)
	if !strings.Contains(sanitized, "`docs/guide.md:12`") {
		t.Fatalf("sanitizeLocalMarkdownLinks() = %q, want workspace-relative local path replacement", sanitized)
	}
	sanitizedMissing := sanitizeLocalMarkdownLinks(
		"See [Missing](./docs/missing.txt:9)",
		workspace,
	)
	if !strings.Contains(sanitizedMissing, "`docs/missing.txt:9`") {
		t.Fatalf("sanitizeLocalMarkdownLinks(missing) = %q, want workspace-relative local path", sanitizedMissing)
	}
	sanitizedAbsolute := sanitizeLocalMarkdownLinks(
		"See [Guide]("+linked+":12)",
		workspace,
	)
	if !strings.Contains(sanitizedAbsolute, "`docs/guide.md:12`") {
		t.Fatalf("sanitizeLocalMarkdownLinks(abs) = %q, want workspace-relative absolute path", sanitizedAbsolute)
	}
	neutralized := neutralizeLocalMarkdownLinks(
		"See [Guide](./docs/guide.md:12) and [Web](https://example.com)",
		workspace,
	)
	if !strings.Contains(neutralized, "`docs/guide.md:12`") || !strings.Contains(neutralized, "[Web](https://example.com)") {
		t.Fatalf("neutralizeLocalMarkdownLinks() = %q, want local de-link + remote keep", neutralized)
	}
	if _, ok := localLinkDisplayTarget("https://example.com/x.md", workspace); ok {
		t.Fatal("localLinkDisplayTarget(https) should be non-local")
	}
	if got, ok := localLinkDisplayTarget("./docs/missing.txt:9", workspace); !ok || got != filepath.Join("docs", "missing.txt")+":9" {
		t.Fatalf("localLinkDisplayTarget(missing) = %q, %v, want workspace-relative path", got, ok)
	}
	if got, ok := localLinkDisplayTarget(linked+":7", workspace); !ok || got != filepath.Join("docs", "guide.md")+":7" {
		t.Fatalf("localLinkDisplayTarget(abs) = %q, %v, want workspace-relative absolute path", got, ok)
	}
	if got := recoverFilenameFromMalformedLabel(".mdguide"); got != "dguide.m" {
		t.Fatalf("recoverFilenameFromMalformedLabel() = %q, want dguide.m", got)
	}
	if !isAlphaNum("abc123") || isAlphaNum("bad-name") {
		t.Fatal("isAlphaNum() returned unexpected result")
	}
	if !isFileNameLike("file_name-1.txt") || isFileNameLike("bad/name") {
		t.Fatal("isFileNameLike() returned unexpected result")
	}
	if got := trimLineReferenceSuffix("/tmp/file.go:10:2"); got != "/tmp/file.go" {
		t.Fatalf("trimLineReferenceSuffix() = %q, want file path", got)
	}
	if !pathWithinWorkspace(linked, workspace) || pathWithinWorkspace(filepath.Join(workspace, "..", "elsewhere"), workspace) {
		t.Fatal("pathWithinWorkspace() returned unexpected result")
	}
	if got := renderWorkspaceDisplayPath(linked+":12", workspace); got != filepath.Join("docs", "guide.md")+":12" {
		t.Fatalf("renderWorkspaceDisplayPath(internal) = %q", got)
	}
	if got := renderWorkspaceDisplayPath(linked+"#L12", workspace); got != filepath.Join("docs", "guide.md")+":12" {
		t.Fatalf("renderWorkspaceDisplayPath(line anchor) = %q", got)
	}
	if got := renderWorkspaceDisplayPath(linked+"#L12C3", workspace); got != filepath.Join("docs", "guide.md")+":12:3" {
		t.Fatalf("renderWorkspaceDisplayPath(line+column anchor) = %q", got)
	}
	if got := renderWorkspaceDisplayPath(filepath.Join(workspace, "..", "elsewhere", "x.go"), workspace); !filepath.IsAbs(got) {
		t.Fatalf("renderWorkspaceDisplayPath(external) = %q, want absolute path", got)
	}
}

func TestDeliveryHelpers(t *testing.T) {
	if title, color, replyClass, showHeader := outboundMessageCardMeta("turn_command_execution"); title != "命令执行" || color != "blue" || replyClass || !showHeader {
		t.Fatalf("outboundMessageCardMeta(turn_command_execution) = %q, %q, %v, %v", title, color, replyClass, showHeader)
	}
	if title, color, replyClass, showHeader := outboundMessageCardMeta("final_message"); title != "最终答复" || color != "green" || !replyClass || !showHeader {
		t.Fatalf("outboundMessageCardMeta(final_message) = %q, %q, %v, %v", title, color, replyClass, showHeader)
	}

	var a *App
	if got := sendFinalMessages(a, nil, nil, "ignored", false); got != nil {
		t.Fatalf("sendFinalMessages(nil app) = %+v, want nil", got)
	}
	a = &App{cfg: config.Default()}
	if got := sendReplyMessages(a, nil, &state.Submission{}, "ignored", false, "final_message"); got != nil {
		t.Fatalf("sendReplyMessages(without feishu) = %+v, want nil", got)
	}
}
