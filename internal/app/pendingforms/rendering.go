package pendingforms

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"feidex/internal/app/apputil"
	appcards "feidex/internal/app/cards"
	"feidex/internal/feishu"
)

// FormDrafts holds intermediate form state for tool user input forms.
type FormDrafts struct {
	Values map[string]string
	Multi  map[string][]string
}

// ---------------------------------------------------------------------------
// Tool User Input – rendering
// ---------------------------------------------------------------------------

// RenderToolUserInputBody builds a markdown body describing all questions in
// the payload. The result is plain text without attention mentions.
func RenderToolUserInputBody(payload ToolUserInputPayload) string {
	lines := []string{"请补充以下输入。"}
	for _, q := range payload.Questions {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s (`%s`)", apputil.FirstNonEmpty(strings.TrimSpace(q.Question), strings.TrimSpace(q.Header), strings.TrimSpace(q.ID)), q.ID))
		if len(q.Options) > 0 {
			opts := make([]string, 0, len(q.Options))
			for _, opt := range q.Options {
				opts = append(opts, opt.Label)
			}
			lines = append(lines, "可选值: "+strings.Join(opts, ", "))
			if q.MultiSelect {
				lines = append(lines, "这是多选题。")
			} else {
				lines = append(lines, "这是单选题。")
			}
			if q.IsOther {
				lines = append(lines, "也可填写其它值。")
			}
		}
		if q.IsSecret {
			lines = append(lines, "注意: 此答案会按敏感输入处理，不写普通日志。")
		}
	}
	if len(payload.Questions) > 0 {
		lines = append(lines, "", "单选题会显示下拉选择，多选题会显示可切换按钮。")
		lines = append(lines, "只有自由文本或其它值输入时，才需要手填文本。")
	}
	return strings.Join(lines, "\n")
}

// RenderToolUserInputQuickBody builds the markdown body for a quick single-
// question card with button answers. The result is plain text without
// attention mentions.
func RenderToolUserInputQuickBody(q ToolUserInputQuestion) string {
	body := strings.TrimSpace(ToolUserInputQuestionMarkdown(q, "", nil, ""))
	if body == "" {
		return "请点击下方按钮完成选择。"
	}
	return "请点击下方按钮完成选择。\n\n" + body
}

// RenderToolUserInputFormCard builds a Feishu card containing a form for tool
// user input questions. attentionUserID is the Feishu user to @-mention.
func RenderToolUserInputFormCard(requestID string, payload ToolUserInputPayload, drafts FormDrafts, attentionUserID string) map[string]any {
	card := appcards.NewMarkdownBodyCard("需要补充输入", "orange")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": apputil.PrependAttentionMentionMarkdown(RenderToolUserInputBody(payload), attentionUserID),
	})
	formElements := make([]map[string]any, 0, len(payload.Questions)+4)
	for _, q := range payload.Questions {
		for _, elem := range RenderToolUserInputQuestionElements(q, drafts, requestID) {
			formElements = append(formElements, elem)
		}
	}
	buttonRows := appcards.BuildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text: "提交",
			Type: "primary",
			Name: "user_input_submit",
			Value: map[string]any{
				"action":       "user_input.answer",
				"request_id":   strings.TrimSpace(requestID),
				"multi_drafts": ToolUserInputMultiDraftActionValue(drafts.Multi),
			},
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "user_input_cancel",
			Value: map[string]any{"action": "pending_form.cancel", "request_id": strings.TrimSpace(requestID)},
		},
	})
	for idx, row := range buttonRows {
		columns, _ := row["columns"].([]map[string]any)
		if len(columns) == 0 {
			continue
		}
		elements, _ := columns[0]["elements"].([]map[string]any)
		if len(elements) == 0 {
			continue
		}
		if idx == 0 {
			elements[0]["form_action_type"] = "submit"
		}
	}
	formElements = append(formElements, buttonRows...)
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":                "form",
		"name":               "tool_user_input_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements":           formElements,
	})
	return card
}

// RenderToolUserInputQuestionElements returns card elements for a single question.
func RenderToolUserInputQuestionElements(q ToolUserInputQuestion, drafts FormDrafts, requestID string) []map[string]any {
	elements := []map[string]any{
		{
			"tag": "markdown",
			"content": ToolUserInputQuestionMarkdown(
				q,
				ToolUserInputDraftValue(drafts, q.ID),
				ToolUserInputMultiDraftValues(drafts, q.ID),
				ToolUserInputDraftValue(drafts, ToolUserInputOtherFieldName(q)),
			),
		},
	}
	switch {
	case len(q.Options) == 0:
		elements = append(elements, BuildToolUserInputTextInputElement(q, drafts))
	case q.MultiSelect:
		elements = append(elements, BuildToolUserInputMultiSelectRows(q, drafts, requestID)...)
	default:
		elements = append(elements, BuildToolUserInputSingleSelectElement(q, drafts))
	}
	if q.IsOther {
		elements = append(elements, BuildToolUserInputOtherInputElement(q, drafts))
	}
	return elements
}

// ToolUserInputQuestionMarkdown returns markdown describing a question and its
// current draft state.
func ToolUserInputQuestionMarkdown(q ToolUserInputQuestion, draftValue string, selected []string, otherValue string) string {
	lines := []string{
		"**" + apputil.FirstNonEmpty(strings.TrimSpace(q.Question), strings.TrimSpace(q.Header), strings.TrimSpace(q.ID)) + "**",
		"`" + strings.TrimSpace(q.ID) + "`",
	}
	if len(q.Options) > 0 {
		mode := "单选"
		if q.MultiSelect {
			mode = "多选"
		}
		lines = append(lines, mode+"题")
		if !q.MultiSelect {
			lines = append(lines, ToolUserInputQuestionOptionMarkdownLines(q)...)
		}
		if len(selected) > 0 {
			lines = append(lines, "当前已选: "+ToolUserInputSummaryText(q, selected, false))
		} else if q.MultiSelect {
			lines = append(lines, "当前已选: `-`")
		} else if strings.TrimSpace(draftValue) != "" {
			lines = append(lines, "当前选择: "+ToolUserInputSummaryText(q, []string{strings.TrimSpace(draftValue)}, false))
		}
	}
	if q.IsOther {
		otherValue = strings.TrimSpace(otherValue)
		switch {
		case otherValue == "":
			lines = append(lines, "可补充其它值。")
		case q.IsSecret:
			lines = append(lines, "其它值: [redacted]")
		default:
			lines = append(lines, "其它值: "+otherValue)
		}
	}
	if q.IsSecret {
		lines = append(lines, "敏感输入会在展示中打码。")
	}
	return strings.Join(lines, "\n")
}

// ToolUserInputQuestionPlaceholder returns placeholder text for a question input.
func ToolUserInputQuestionPlaceholder(q ToolUserInputQuestion) string {
	title := apputil.FirstNonEmpty(strings.TrimSpace(q.Question), strings.TrimSpace(q.Header), strings.TrimSpace(q.ID), "请输入答案")
	if len(q.Options) == 0 {
		return title
	}
	opts := make([]string, 0, len(q.Options))
	for _, opt := range q.Options {
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			continue
		}
		opts = append(opts, label)
		if len(opts) >= 3 {
			break
		}
	}
	if len(opts) == 0 {
		return title
	}
	suffix := "例如: " + strings.Join(opts, ", ")
	if q.IsOther {
		suffix += "，也可填写其它值"
	}
	return title + " | " + suffix
}

// BuildToolUserInputTextInputElement builds a text input element for free-form questions.
func BuildToolUserInputTextInputElement(q ToolUserInputQuestion, drafts FormDrafts) map[string]any {
	input := map[string]any{
		"tag":         "input",
		"name":        q.ID,
		"required":    true,
		"placeholder": map[string]any{"tag": "plain_text", "content": ToolUserInputQuestionPlaceholder(q)},
	}
	if value := ToolUserInputDraftValue(drafts, q.ID); value != "" {
		input["default_value"] = value
	}
	return input
}

// BuildToolUserInputSingleSelectElement builds a single-select dropdown element.
func BuildToolUserInputSingleSelectElement(q ToolUserInputQuestion, drafts FormDrafts) map[string]any {
	options := make([]appcards.SelectStaticOption, 0, len(q.Options))
	for _, opt := range q.Options {
		options = append(options, appcards.SelectStaticOption{
			Text:  ToolUserInputOptionText(opt),
			Value: strings.TrimSpace(opt.Label),
		})
	}
	initialOption := ToolUserInputInitialOption(q, ToolUserInputDraftValue(drafts, q.ID))
	return appcards.BuildFormSelectStaticElement(q.ID, ToolUserInputQuestionPlaceholder(q), options, initialOption)
}

// BuildToolUserInputOtherInputElement builds a text input for "other" value.
func BuildToolUserInputOtherInputElement(q ToolUserInputQuestion, drafts FormDrafts) map[string]any {
	input := map[string]any{
		"tag":         "input",
		"name":        ToolUserInputOtherFieldName(q),
		"required":    false,
		"placeholder": map[string]any{"tag": "plain_text", "content": "其它值（可选）"},
	}
	if value := ToolUserInputDraftValue(drafts, ToolUserInputOtherFieldName(q)); value != "" {
		input["default_value"] = value
	}
	return input
}

// BuildToolUserInputMultiSelectRows builds toggle button rows for multi-select questions.
func BuildToolUserInputMultiSelectRows(q ToolUserInputQuestion, drafts FormDrafts, requestID string) []map[string]any {
	selected := ToolUserInputMultiDraftValues(drafts, q.ID)
	selectedSet := map[string]struct{}{}
	for _, value := range selected {
		selectedSet[strings.TrimSpace(value)] = struct{}{}
	}
	buttons := make([]feishu.Button, 0, len(q.Options))
	for _, opt := range q.Options {
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			continue
		}
		text := "[ ] " + label
		buttonType := "default"
		if _, ok := selectedSet[label]; ok {
			text = "[x] " + label
			buttonType = "primary"
		}
		buttons = append(buttons, feishu.Button{
			Text: text,
			Type: buttonType,
			Name: "toggle_" + strings.TrimSpace(q.ID) + "_" + strings.ToLower(strings.ReplaceAll(label, " ", "_")),
			Value: map[string]any{
				"action":       "user_input.toggle_multi",
				"request_id":   strings.TrimSpace(requestID),
				"question_id":  q.ID,
				"option_label": label,
				"multi_drafts": ToolUserInputMultiDraftActionValue(drafts.Multi),
			},
		})
	}
	rows := make([]map[string]any, 0, (len(buttons)+2)/3)
	for start := 0; start < len(buttons); start += 3 {
		end := start + 3
		if end > len(buttons) {
			end = len(buttons)
		}
		rows = append(rows, appcards.BuildMarkdownBodyCardActionElement(buttons[start:end]))
	}
	return rows
}

// ToolUserInputOptionText returns display text for an option (label - description).
func ToolUserInputOptionText(opt ToolUserInputOption) string {
	label := strings.TrimSpace(opt.Label)
	desc := strings.TrimSpace(opt.Description)
	if label == "" || desc == "" {
		return apputil.FirstNonEmpty(label, desc)
	}
	return label + " - " + desc
}

func ToolUserInputQuestionOptionMarkdownLines(q ToolUserInputQuestion) []string {
	lines := make([]string, 0, len(q.Options)+1)
	if len(q.Options) == 0 {
		return lines
	}
	lines = append(lines, "选项:")
	index := 1
	for _, opt := range q.Options {
		text := strings.TrimSpace(ToolUserInputOptionText(opt))
		if text == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d. %s", index, text))
		index++
	}
	return lines
}

func ToolUserInputSummaryText(q ToolUserInputQuestion, answers []string, redact bool) string {
	if redact {
		return "[redacted]"
	}
	rendered := make([]string, 0, len(answers))
	for _, answer := range answers {
		answer = strings.TrimSpace(answer)
		if answer == "" {
			continue
		}
		rendered = append(rendered, ToolUserInputDisplayValue(q, answer))
	}
	return strings.Join(rendered, ", ")
}

func ToolUserInputDisplayValue(q ToolUserInputQuestion, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, opt := range q.Options {
		if strings.EqualFold(strings.TrimSpace(opt.Label), raw) {
			return ToolUserInputOptionText(opt)
		}
	}
	return raw
}

// ToolUserInputInitialOption returns the canonical option label matching raw, or "".
func ToolUserInputInitialOption(q ToolUserInputQuestion, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, opt := range q.Options {
		if strings.EqualFold(strings.TrimSpace(opt.Label), raw) {
			return strings.TrimSpace(opt.Label)
		}
	}
	return ""
}

// ToolUserInputOtherFieldName returns the form field name for "other" input.
func ToolUserInputOtherFieldName(q ToolUserInputQuestion) string {
	return strings.TrimSpace(q.ID) + "__other"
}

// ---------------------------------------------------------------------------
// Tool User Input – parsing
// ---------------------------------------------------------------------------

// ParseToolUserInputResponse parses a structured text response into a reply
// payload and summary string.
func ParseToolUserInputResponse(text string, payload ToolUserInputPayload) (map[string]any, string, error) {
	answerMap := ParseStructuredLines(text)
	selections := make(map[string]string, len(payload.Questions))
	for _, q := range payload.Questions {
		raw := strings.TrimSpace(answerMap[q.ID])
		if raw == "" && len(payload.Questions) == 1 {
			raw = text
		}
		selections[q.ID] = raw
	}
	return BuildToolUserInputResponseFromSelections(payload, selections)
}

// ParseQuestionAnswers validates and normalizes answers for a single question.
func ParseQuestionAnswers(raw string, q ToolUserInputQuestion) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("answer is required")
	}
	if len(q.Options) == 0 {
		return []string{raw}, nil
	}
	parts := SplitAnswerParts(raw)
	allowed := map[string]string{}
	for _, opt := range q.Options {
		allowed[strings.ToLower(strings.TrimSpace(opt.Label))] = opt.Label
	}
	var answers []string
	for _, part := range parts {
		if matched, ok := allowed[strings.ToLower(part)]; ok {
			answers = append(answers, matched)
			continue
		}
		if q.IsOther {
			answers = append(answers, part)
			continue
		}
		return nil, fmt.Errorf("unsupported option %q", part)
	}
	if len(answers) == 0 {
		return nil, fmt.Errorf("answer is required")
	}
	if !q.MultiSelect && len(answers) > 1 {
		return nil, fmt.Errorf("only one answer is allowed")
	}
	return answers, nil
}

// SplitAnswerParts splits a raw answer string by commas and newlines.
func SplitAnswerParts(raw string) []string {
	raw = strings.ReplaceAll(raw, "\n", ",")
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// SummarizeAnswers returns a display summary; secrets are redacted.
func SummarizeAnswers(answers []string, secret bool) string {
	if secret {
		return "[redacted]"
	}
	return strings.Join(answers, ", ")
}

// BuildToolUserInputResponseFromSelections builds a reply payload from question selections.
func BuildToolUserInputResponseFromSelections(payload ToolUserInputPayload, selections map[string]string) (map[string]any, string, error) {
	result := map[string]any{"answers": map[string]any{}}
	summaryLines := make([]string, 0, len(payload.Questions))
	for _, q := range payload.Questions {
		raw := strings.TrimSpace(selections[q.ID])
		answers, err := ParseQuestionAnswers(raw, q)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", q.ID, err)
		}
		result["answers"].(map[string]any)[q.ID] = map[string]any{"answers": answers}
		summaryLines = append(summaryLines, fmt.Sprintf("`%s`: %s", q.ID, ToolUserInputSummaryText(q, answers, q.IsSecret)))
	}
	return result, strings.Join(summaryLines, "\n"), nil
}

// ---------------------------------------------------------------------------
// Tool User Input – drafts helpers
// ---------------------------------------------------------------------------

// ToolUserInputDraftsFromCardAction extracts form drafts from a card action.
func ToolUserInputDraftsFromCardAction(payload ToolUserInputPayload, action *feishu.CardAction) FormDrafts {
	drafts := FormDrafts{
		Values: map[string]string{},
		Multi:  ToolUserInputMultiDraftsFromActionValue(ActionValueMap(action)),
	}
	for _, q := range payload.Questions {
		if value, ok := ToolUserInputSelectionValue(action.FormValue, q.ID); ok {
			drafts.Values[q.ID] = value
		}
		if q.IsOther {
			if value, ok := ToolUserInputSelectionValue(action.FormValue, ToolUserInputOtherFieldName(q)); ok {
				drafts.Values[ToolUserInputOtherFieldName(q)] = value
			}
		}
	}
	return drafts
}

// ToolUserInputSelectionsFromDrafts converts drafts to a selections map.
func ToolUserInputSelectionsFromDrafts(payload ToolUserInputPayload, drafts FormDrafts) map[string]string {
	selections := make(map[string]string, len(payload.Questions))
	for _, q := range payload.Questions {
		switch {
		case q.MultiSelect:
			parts := append([]string(nil), ToolUserInputMultiDraftValues(drafts, q.ID)...)
			if q.IsOther {
				parts = append(parts, SplitAnswerParts(ToolUserInputDraftValue(drafts, ToolUserInputOtherFieldName(q)))...)
			}
			selections[q.ID] = strings.Join(parts, ", ")
		case len(q.Options) > 0:
			parts := SplitAnswerParts(ToolUserInputDraftValue(drafts, q.ID))
			if q.IsOther {
				parts = append(parts, SplitAnswerParts(ToolUserInputDraftValue(drafts, ToolUserInputOtherFieldName(q)))...)
			}
			selections[q.ID] = strings.Join(parts, ", ")
		default:
			selections[q.ID] = ToolUserInputDraftValue(drafts, q.ID)
		}
	}
	return selections
}

// ToolUserInputDraftValue returns the trimmed draft value for a key.
func ToolUserInputDraftValue(drafts FormDrafts, key string) string {
	if drafts.Values == nil {
		return ""
	}
	return strings.TrimSpace(drafts.Values[strings.TrimSpace(key)])
}

// ToolUserInputMultiDraftValues returns trimmed multi-draft values for a key.
func ToolUserInputMultiDraftValues(drafts FormDrafts, key string) []string {
	if drafts.Multi == nil {
		return nil
	}
	values := drafts.Multi[strings.TrimSpace(key)]
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// ToolUserInputMultiDraftActionValue converts multi-drafts to an action value map.
func ToolUserInputMultiDraftActionValue(drafts map[string][]string) map[string]any {
	if len(drafts) == 0 {
		return map[string]any{}
	}
	keys := make([]string, 0, len(drafts))
	for key := range drafts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		values := drafts[key]
		if len(values) == 0 {
			continue
		}
		copied := make([]any, 0, len(values))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" {
				copied = append(copied, value)
			}
		}
		if len(copied) > 0 {
			out[key] = copied
		}
	}
	return out
}

// ToolUserInputMultiDraftsFromActionValue extracts multi-drafts from an action value.
func ToolUserInputMultiDraftsFromActionValue(values map[string]any) map[string][]string {
	raw, ok := values["multi_drafts"]
	if !ok {
		return map[string][]string{}
	}
	result := map[string][]string{}
	switch typed := raw.(type) {
	case map[string]any:
		for key, value := range typed {
			if values := ToolUserInputMultiDraftList(value); len(values) > 0 {
				result[strings.TrimSpace(key)] = values
			}
		}
	case map[string]string:
		for key, value := range typed {
			if values := SplitAnswerParts(value); len(values) > 0 {
				result[strings.TrimSpace(key)] = values
			}
		}
	}
	return result
}

// ToolUserInputMultiDraftList normalizes a raw value into a string slice.
func ToolUserInputMultiDraftList(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return UniqueToolUserInputParts(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := NormalizeToolUserInputSelection(item); ok && strings.TrimSpace(value) != "" {
				parts = append(parts, strings.TrimSpace(value))
			}
		}
		return UniqueToolUserInputParts(parts)
	case string:
		return UniqueToolUserInputParts(SplitAnswerParts(typed))
	}
	return nil
}

// UniqueToolUserInputParts deduplicates parts case-insensitively preserving order.
func UniqueToolUserInputParts(parts []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(part)]; ok {
			continue
		}
		seen[strings.ToLower(part)] = struct{}{}
		out = append(out, part)
	}
	return out
}

// ToggleToolUserInputMultiDraft toggles an option in a multi-select draft.
func ToggleToolUserInputMultiDraft(drafts FormDrafts, questionID, optionLabel string) FormDrafts {
	if drafts.Multi == nil {
		drafts.Multi = map[string][]string{}
	}
	questionID = strings.TrimSpace(questionID)
	optionLabel = strings.TrimSpace(optionLabel)
	current := ToolUserInputMultiDraftValues(drafts, questionID)
	next := make([]string, 0, len(current))
	found := false
	for _, value := range current {
		if strings.EqualFold(value, optionLabel) {
			found = true
			continue
		}
		next = append(next, value)
	}
	if !found && optionLabel != "" {
		next = append(next, optionLabel)
	}
	if len(next) == 0 {
		delete(drafts.Multi, questionID)
		return drafts
	}
	drafts.Multi[questionID] = next
	return drafts
}

// ActionValueMap returns the action value map from a card action, or an empty map.
func ActionValueMap(action *feishu.CardAction) map[string]any {
	if action == nil || action.ActionValue == nil {
		return map[string]any{}
	}
	return action.ActionValue
}

// ToolUserInputSelectionValue extracts a normalized string value from a form values map.
func ToolUserInputSelectionValue(values map[string]any, key string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	raw, ok := values[key]
	if !ok {
		return "", false
	}
	return NormalizeToolUserInputSelection(raw)
}

// NormalizeToolUserInputSelection converts a raw form value to a trimmed string.
func NormalizeToolUserInputSelection(raw any) (string, bool) {
	switch v := raw.(type) {
	case nil:
		return "", false
	case string:
		return strings.TrimSpace(v), true
	case []string:
		return strings.TrimSpace(strings.Join(v, ", ")), true
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			part, ok := NormalizeToolUserInputSelection(item)
			if ok && part != "" {
				parts = append(parts, part)
			}
		}
		return strings.TrimSpace(strings.Join(parts, ", ")), true
	case map[string]any:
		if value, ok := v["value"]; ok {
			return NormalizeToolUserInputSelection(value)
		}
	}
	return strings.TrimSpace(fmt.Sprint(raw)), true
}

// ---------------------------------------------------------------------------
// Elicitation Form – rendering
// ---------------------------------------------------------------------------

// RenderElicitationFormBody builds a markdown body describing an elicitation form.
func RenderElicitationFormBody(payload ElicitationFormPayload) string {
	lines := []string{payload.Message, "", "请直接回复下一条消息提交表单。"}
	if spec, err := ParseElicitationSchema(payload.Schema); err == nil {
		for _, field := range spec.Fields {
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("%s%s", apputil.FirstNonEmpty(strings.TrimSpace(field.Title), strings.TrimSpace(field.Name)), RequiredMarker(field.Required)))
			if strings.TrimSpace(field.Description) != "" {
				lines = append(lines, strings.TrimSpace(field.Description))
			}
			if details := elicitationFieldDetailLines(field); len(details) > 0 {
				lines = append(lines, details...)
			}
		}
	} else if properties, _ := payload.Schema["properties"].(map[string]any); len(properties) > 0 {
		keys := SortedMapKeys(properties)
		required := RequiredSet(payload.Schema)
		for _, key := range keys {
			field, _ := properties[key].(map[string]any)
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("%s%s", DisplayFieldTitle(key, field), RequiredMarker(required[key])))
			if desc := StringField(field, "description"); desc != "" {
				lines = append(lines, desc)
			}
			if options := SchemaOptionLabels(field); len(options) > 0 {
				lines = append(lines, "可选值: "+strings.Join(options, ", "))
			}
		}
	}
	lines = append(lines, "", "多字段请按以下格式：", "field_name: value")
	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Elicitation Form – parsing
// ---------------------------------------------------------------------------

// ParseElicitationFormResponse parses a structured text response into content
// and a summary string.
func ParseElicitationFormResponse(text string, payload ElicitationFormPayload) (map[string]any, string, error) {
	spec, err := ParseElicitationSchema(payload.Schema)
	if err != nil {
		return nil, "", err
	}
	answerMap := ParseStructuredLines(text)
	content := map[string]any{}
	summaryLines := make([]string, 0, len(spec.Fields))
	for _, field := range spec.Fields {
		raw := strings.TrimSpace(answerMap[field.Name])
		provided := raw != ""
		if raw == "" && len(spec.Fields) == 1 && len(answerMap) == 0 {
			raw = text
			provided = strings.TrimSpace(text) != ""
		}
		if field.Kind == ElicitationFieldMultiSelect {
			values, summary, include, err := normalizeElicitationMultiValue(field, SplitAnswerParts(raw), provided)
			if err != nil {
				return nil, "", fmt.Errorf("%s: %w", field.Name, err)
			}
			if include {
				content[field.Name] = values
				summaryLines = append(summaryLines, fmt.Sprintf("`%s`: %s", field.Name, summary))
			}
			continue
		}
		value, summary, include, err := normalizeElicitationTextValue(field, raw, provided)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", field.Name, err)
		}
		if include {
			content[field.Name] = value
			summaryLines = append(summaryLines, fmt.Sprintf("`%s`: %s", field.Name, summary))
		}
	}
	return content, strings.Join(summaryLines, "\n"), nil
}

// ParseElicitationFieldValue parses a raw string according to the field schema type.
func ParseElicitationFieldValue(raw string, field map[string]any) (any, string, error) {
	raw = strings.TrimSpace(raw)
	switch FieldType(field) {
	case "boolean":
		v, err := ParseBool(raw)
		if err != nil {
			return nil, "", err
		}
		return v, strconv.FormatBool(v), nil
	case "number":
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, "", fmt.Errorf("expect number")
		}
		return v, raw, nil
	case "integer":
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("expect integer")
		}
		return v, raw, nil
	case "array":
		parts := SplitAnswerParts(raw)
		if len(parts) == 0 {
			return nil, "", fmt.Errorf("expect one or more values")
		}
		return parts, strings.Join(parts, ", "), nil
	default:
		if options := SchemaOptionLabels(field); len(options) > 0 {
			matched, err := MatchSchemaOption(raw, field)
			if err != nil {
				return nil, "", err
			}
			return matched, matched, nil
		}
		return raw, raw, nil
	}
}

// ---------------------------------------------------------------------------
// Elicitation Form – schema helpers
// ---------------------------------------------------------------------------

// RequiredSet returns the set of required field names from a schema.
func RequiredSet(schema map[string]any) map[string]bool {
	required := map[string]bool{}
	switch values := schema["required"].(type) {
	case []any:
		for _, v := range values {
			if s, ok := v.(string); ok {
				required[s] = true
			}
		}
	case []string:
		for _, s := range values {
			required[s] = true
		}
	}
	return required
}

// SortedMapKeys returns the sorted keys of a map.
func SortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// DisplayFieldTitle returns the display title for a field, falling back to key.
func DisplayFieldTitle(key string, field map[string]any) string {
	if title := StringField(field, "title"); title != "" {
		return title
	}
	return key
}

// RequiredMarker returns " *" if required, else "".
func RequiredMarker(required bool) string {
	if required {
		return " *"
	}
	return ""
}

// StringField extracts a trimmed string value from a field map.
func StringField(field map[string]any, key string) string {
	if field == nil {
		return ""
	}
	if v, ok := field[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// FieldType returns the "type" value from a field map.
func FieldType(field map[string]any) string {
	if field == nil {
		return ""
	}
	if v, ok := field["type"].(string); ok {
		return v
	}
	return ""
}

// SchemaOptionLabels returns display labels for enum/oneOf/anyOf options.
func SchemaOptionLabels(field map[string]any) []string {
	switch {
	case field == nil:
		return nil
	case field["enum"] != nil:
		values, _ := field["enum"].([]any)
		out := make([]string, 0, len(values))
		names, _ := field["enumNames"].([]any)
		for i, v := range values {
			value, _ := v.(string)
			label := value
			if i < len(names) {
				if s, ok := names[i].(string); ok && strings.TrimSpace(s) != "" {
					label = s
				}
			}
			out = append(out, label)
		}
		return out
	case field["oneOf"] != nil:
		values, _ := field["oneOf"].([]any)
		out := make([]string, 0, len(values))
		for _, v := range values {
			item, _ := v.(map[string]any)
			if title := StringField(item, "title"); title != "" {
				out = append(out, title)
				continue
			}
			if c := StringField(item, "const"); c != "" {
				out = append(out, c)
			}
		}
		return out
	case field["items"] != nil:
		items, _ := field["items"].(map[string]any)
		if anyOf, _ := items["anyOf"].([]any); len(anyOf) > 0 {
			out := make([]string, 0, len(anyOf))
			for _, v := range anyOf {
				item, _ := v.(map[string]any)
				if title := StringField(item, "title"); title != "" {
					out = append(out, title)
					continue
				}
				if c := StringField(item, "const"); c != "" {
					out = append(out, c)
				}
			}
			return out
		}
	}
	return nil
}

// MatchSchemaOption matches a raw input string against schema options,
// returning the canonical value.
func MatchSchemaOption(raw string, field map[string]any) (string, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case field["enum"] != nil:
		values, _ := field["enum"].([]any)
		names, _ := field["enumNames"].([]any)
		for i, v := range values {
			value, _ := v.(string)
			if strings.EqualFold(raw, value) {
				return value, nil
			}
			if i < len(names) {
				if label, ok := names[i].(string); ok && strings.EqualFold(raw, label) {
					return value, nil
				}
			}
		}
	case field["oneOf"] != nil:
		values, _ := field["oneOf"].([]any)
		for _, v := range values {
			item, _ := v.(map[string]any)
			value := StringField(item, "const")
			title := StringField(item, "title")
			if strings.EqualFold(raw, value) || (title != "" && strings.EqualFold(raw, title)) {
				return value, nil
			}
		}
	case field["items"] != nil:
		return raw, nil
	}
	return "", fmt.Errorf("unsupported option %q", raw)
}

// ParseBool parses a boolean from various string representations.
func ParseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "yes", "y", "1", "是":
		return true, nil
	case "false", "no", "n", "0", "否":
		return false, nil
	default:
		return false, fmt.Errorf("expect boolean yes/no")
	}
}
