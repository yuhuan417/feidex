package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/state"
)

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
	if !strings.Contains(body, "注意: 此答案会按敏感输入处理") || !strings.Contains(body, "question_id: answer") {
		t.Fatalf("renderToolUserInputBody() missing secret or multi-question hint:\n%s", body)
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
	answers, err := parseQuestionAnswers("fast, safe", question)
	if err != nil {
		t.Fatalf("parseQuestionAnswers(options) error = %v", err)
	}
	if len(answers) != 2 || answers[0] != "Fast" || answers[1] != "Safe" {
		t.Fatalf("parseQuestionAnswers(options) = %+v, want canonical labels", answers)
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

	sub := &state.Submission{
		InputText: "hello",
		Attachments: []state.SubmissionAttachment{
			{Kind: "image", LocalPath: "/tmp/image.png"},
			{Kind: "file", LocalPath: "/tmp/doc.txt"},
			{Kind: "audio", LocalPath: "/tmp/audio.wav"},
		},
	}
	inputs := buildTurnInputs(sub)
	if len(inputs) != 4 || inputs[0]["type"] != "text" || inputs[1]["type"] != "localImage" {
		t.Fatalf("buildTurnInputs() = %+v, want text + localImage + prompts", inputs)
	}
	if got := attachmentPrompt(state.SubmissionAttachment{Kind: "audio", LocalPath: "/tmp/a.wav"}); !strings.Contains(got, "audio file") {
		t.Fatalf("attachmentPrompt(audio) = %q, want audio text", got)
	}
	if got := attachmentPrompt(state.SubmissionAttachment{}); got != "" {
		t.Fatalf("attachmentPrompt(empty) = %q, want empty", got)
	}

	preview := submissionInputPreview(&state.Submission{
		InputText: "Question",
		Attachments: []state.SubmissionAttachment{
			{Kind: "image", Name: "pic.png"},
			{Kind: "file", LocalPath: "/tmp/report.pdf"},
		},
	})
	if !strings.Contains(preview, "Question") || !strings.Contains(preview, "[图片] pic.png") || !strings.Contains(preview, "[文件] report.pdf") {
		t.Fatalf("submissionInputPreview() = %q, want text and attachment previews", preview)
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
	if !strings.Contains(sanitized, "`./docs/guide.md:12`") {
		t.Fatalf("sanitizeLocalMarkdownLinks() = %q, want full local path replacement", sanitized)
	}
	sanitizedMissing := sanitizeLocalMarkdownLinks(
		"See [Missing](./docs/missing.txt:9)",
		workspace,
	)
	if !strings.Contains(sanitizedMissing, "`./docs/missing.txt:9`") {
		t.Fatalf("sanitizeLocalMarkdownLinks(missing) = %q, want raw local path", sanitizedMissing)
	}
	neutralized := neutralizeLocalMarkdownLinks(
		"See [Guide](./docs/guide.md:12) and [Web](https://example.com)",
		workspace,
	)
	if !strings.Contains(neutralized, "`./docs/guide.md:12`") || !strings.Contains(neutralized, "[Web](https://example.com)") {
		t.Fatalf("neutralizeLocalMarkdownLinks() = %q, want local de-link + remote keep", neutralized)
	}
	if _, ok := localLinkDisplayTarget("https://example.com/x.md", workspace); ok {
		t.Fatal("localLinkDisplayTarget(https) should be non-local")
	}
	if got, ok := localLinkDisplayTarget("./docs/missing.txt:9", workspace); !ok || got != "./docs/missing.txt:9" {
		t.Fatalf("localLinkDisplayTarget(missing) = %q, %v, want raw local path", got, ok)
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
}

func TestDeliveryHelpers(t *testing.T) {
	if title, color, replyClass, showHeader := outboundMessageCardMeta("turn_command_execution"); title != "命令执行" || color != "blue" || replyClass || !showHeader {
		t.Fatalf("outboundMessageCardMeta(turn_command_execution) = %q, %q, %v, %v", title, color, replyClass, showHeader)
	}
	if title, color, replyClass, showHeader := outboundMessageCardMeta("final_message"); title != "最终答复" || color != "green" || !replyClass || !showHeader {
		t.Fatalf("outboundMessageCardMeta(final_message) = %q, %q, %v, %v", title, color, replyClass, showHeader)
	}

	var a *App
	if got := a.sendFinalMessages(nil, nil, "ignored", false); got != nil {
		t.Fatalf("sendFinalMessages(nil app) = %+v, want nil", got)
	}
	a = &App{cfg: config.Default()}
	if got := a.sendReplyMessages(nil, &state.Submission{}, "ignored", false, "final_message"); got != nil {
		t.Fatalf("sendReplyMessages(without feishu) = %+v, want nil", got)
	}
}
