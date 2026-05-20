package pendingforms

import (
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"feidex/internal/app/apputil"
	appcards "feidex/internal/app/cards"
	"feidex/internal/feishu"
)

type ElicitationFieldKind string

const (
	ElicitationFieldString       ElicitationFieldKind = "string"
	ElicitationFieldNumber       ElicitationFieldKind = "number"
	ElicitationFieldInteger      ElicitationFieldKind = "integer"
	ElicitationFieldBoolean      ElicitationFieldKind = "boolean"
	ElicitationFieldSingleSelect ElicitationFieldKind = "single_select"
	ElicitationFieldMultiSelect  ElicitationFieldKind = "multi_select"
)

type ElicitationRenderMode string

const (
	ElicitationRenderModeDecision ElicitationRenderMode = "decision"
	ElicitationRenderModeConfirm  ElicitationRenderMode = "confirm"
	ElicitationRenderModeInline   ElicitationRenderMode = "inline"
)

type ElicitationOption struct {
	Value string
	Label string
}

type ElicitationField struct {
	Name        string
	Title       string
	Description string
	Kind        ElicitationFieldKind
	Required    bool
	Format      string
	Options     []ElicitationOption

	DefaultText  string
	HasDefault   bool
	DefaultMulti []string

	MinLength *int
	MaxLength *int
	Minimum   *float64
	Maximum   *float64
	MinItems  *int
	MaxItems  *int
}

type ElicitationFormSpec struct {
	Fields     []ElicitationField
	RenderMode ElicitationRenderMode
}

type ElicitationCardRenderResult struct {
	Card           map[string]any
	TextFallback   bool
	FallbackReason string
}

// RenderElicitationRequestCard renders the best available card for an MCP
// elicitation request. Unsupported schemas fall back to plain text guidance.
func RenderElicitationRequestCard(requestID string, payload ElicitationFormPayload, drafts FormDrafts, attentionUserID string) ElicitationCardRenderResult {
	spec, err := ParseElicitationSchema(payload.Schema)
	if err != nil {
		return ElicitationCardRenderResult{
			Card:           renderElicitationTextFallbackCard(payload, attentionUserID, err.Error(), requestID),
			TextFallback:   true,
			FallbackReason: err.Error(),
		}
	}
	switch spec.RenderMode {
	case ElicitationRenderModeConfirm:
		return ElicitationCardRenderResult{
			Card: renderElicitationConfirmCard(requestID, payload, attentionUserID),
		}
	case ElicitationRenderModeDecision:
		return ElicitationCardRenderResult{
			Card: renderElicitationDecisionCard(requestID, payload, spec, attentionUserID),
		}
	default:
		return ElicitationCardRenderResult{
			Card: renderElicitationInlineFormCard(requestID, payload, spec, drafts, attentionUserID),
		}
	}
}

// ParseElicitationSchema normalizes a supported subset of the official
// McpElicitationSchema into field definitions Feidex can render and validate.
func ParseElicitationSchema(schema map[string]any) (ElicitationFormSpec, error) {
	if len(schema) == 0 {
		return ElicitationFormSpec{}, fmt.Errorf("empty elicitation schema")
	}
	if schemaType := strings.TrimSpace(StringField(schema, "type")); schemaType != "" && schemaType != "object" {
		return ElicitationFormSpec{}, fmt.Errorf("unsupported elicitation schema type %q", schemaType)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return ElicitationFormSpec{}, fmt.Errorf("elicitation schema must define properties")
	}
	if len(properties) == 0 {
		return ElicitationFormSpec{RenderMode: ElicitationRenderModeConfirm}, nil
	}
	required := RequiredSet(schema)
	fields := make([]ElicitationField, 0, len(properties))
	for _, key := range SortedMapKeys(properties) {
		rawField, _ := properties[key].(map[string]any)
		field, err := parseElicitationField(key, rawField, required[key])
		if err != nil {
			return ElicitationFormSpec{}, fmt.Errorf("%s: %w", key, err)
		}
		fields = append(fields, field)
	}
	spec := ElicitationFormSpec{Fields: fields, RenderMode: ElicitationRenderModeInline}
	if len(fields) == 1 {
		field := fields[0]
		if field.Kind == ElicitationFieldBoolean {
			spec.RenderMode = ElicitationRenderModeDecision
		}
		if field.Kind == ElicitationFieldSingleSelect && len(field.Options) > 0 && len(field.Options) <= 5 {
			spec.RenderMode = ElicitationRenderModeDecision
		}
	}
	return spec, nil
}

// ElicitationDraftsFromCardAction extracts typed drafts from an elicitation card action.
func ElicitationDraftsFromCardAction(payload ElicitationFormPayload, action *feishu.CardAction) FormDrafts {
	spec, err := ParseElicitationSchema(payload.Schema)
	if err != nil {
		return FormDrafts{Values: map[string]string{}, Multi: map[string][]string{}}
	}
	drafts := FormDrafts{
		Values: map[string]string{},
		Multi:  ToolUserInputMultiDraftsFromActionValue(ActionValueMap(action)),
	}
	for _, field := range spec.Fields {
		if field.Kind == ElicitationFieldMultiSelect {
			continue
		}
		if value, ok := ToolUserInputSelectionValue(action.FormValue, field.Name); ok {
			drafts.Values[field.Name] = value
		}
	}
	return drafts
}

// BuildElicitationResponseFromDrafts validates card drafts and returns reply content.
func BuildElicitationResponseFromDrafts(payload ElicitationFormPayload, drafts FormDrafts) (map[string]any, string, error) {
	spec, err := ParseElicitationSchema(payload.Schema)
	if err != nil {
		return nil, "", err
	}
	content := map[string]any{}
	summaryLines := make([]string, 0, len(spec.Fields))
	for _, field := range spec.Fields {
		switch field.Kind {
		case ElicitationFieldMultiSelect:
			values, hasValues := elicitationMultiDraftValues(drafts, field)
			normalized, summary, include, err := normalizeElicitationMultiValue(field, values, hasValues)
			if err != nil {
				return nil, "", fmt.Errorf("%s: %w", field.Name, err)
			}
			if include {
				content[field.Name] = normalized
				summaryLines = append(summaryLines, fmt.Sprintf("`%s`: %s", field.Name, summary))
			}
		default:
			raw, hasValue := elicitationDraftValue(drafts, field)
			normalized, summary, include, err := normalizeElicitationTextValue(field, raw, hasValue)
			if err != nil {
				return nil, "", fmt.Errorf("%s: %w", field.Name, err)
			}
			if include {
				content[field.Name] = normalized
				summaryLines = append(summaryLines, fmt.Sprintf("`%s`: %s", field.Name, summary))
			}
		}
	}
	return content, strings.Join(summaryLines, "\n"), nil
}

// NormalizeElicitationQuickAnswer validates a quick button answer for a
// single-field decision card.
func NormalizeElicitationQuickAnswer(field ElicitationField, answer string) (any, string, bool, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		if field.Required {
			return nil, "", false, fmt.Errorf("answer is required")
		}
		return nil, "", false, nil
	}
	switch field.Kind {
	case ElicitationFieldBoolean, ElicitationFieldSingleSelect:
		return normalizeElicitationTextValue(field, answer, true)
	default:
		return nil, "", false, fmt.Errorf("field does not support quick answers")
	}
}

func renderElicitationInlineFormCard(requestID string, payload ElicitationFormPayload, spec ElicitationFormSpec, drafts FormDrafts, attentionUserID string) map[string]any {
	card := appcards.NewMarkdownBodyCard("需要补充表单", "orange")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": apputil.PrependAttentionMentionMarkdown(renderElicitationInlineIntro(payload, spec), attentionUserID),
	})
	formElements := make([]map[string]any, 0, len(spec.Fields)*3+4)
	for _, field := range spec.Fields {
		for _, elem := range renderElicitationFieldElements(field, drafts, requestID) {
			formElements = append(formElements, elem)
		}
	}
	buttonRows := appcards.BuildMarkdownBodyCardActionElements([]feishu.Button{
		{
			Text: "提交",
			Type: "primary",
			Name: "elicitation_form_submit",
			Value: map[string]any{
				"action":       "elicitation_form.answer",
				"request_id":   strings.TrimSpace(requestID),
				"multi_drafts": ToolUserInputMultiDraftActionValue(drafts.Multi),
			},
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "elicitation_form_cancel",
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
		"name":               "elicitation_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements":           formElements,
	})
	return card
}

func renderElicitationDecisionCard(requestID string, payload ElicitationFormPayload, spec ElicitationFormSpec, attentionUserID string) map[string]any {
	field := spec.Fields[0]
	card := appcards.NewMarkdownBodyCard("需要补充表单", "orange")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": apputil.PrependAttentionMentionMarkdown(renderElicitationDecisionIntro(payload, field), attentionUserID),
	})
	buttons := make([]feishu.Button, 0, len(field.Options)+3)
	if field.Kind == ElicitationFieldBoolean {
		buttons = append(buttons,
			feishu.Button{
				Text: "是",
				Type: "primary",
				Name: "elicitation_bool_true",
				Value: map[string]any{
					"action":     "elicitation_form.answer",
					"request_id": strings.TrimSpace(requestID),
					"field_name": field.Name,
					"answer":     "true",
				},
			},
			feishu.Button{
				Text: "否",
				Type: "default",
				Name: "elicitation_bool_false",
				Value: map[string]any{
					"action":     "elicitation_form.answer",
					"request_id": strings.TrimSpace(requestID),
					"field_name": field.Name,
					"answer":     "false",
				},
			},
		)
	} else {
		for idx, opt := range field.Options {
			buttons = append(buttons, feishu.Button{
				Text: opt.Label,
				Type: "primary",
				Name: fmt.Sprintf("elicitation_choice_%d", idx),
				Value: map[string]any{
					"action":     "elicitation_form.answer",
					"request_id": strings.TrimSpace(requestID),
					"field_name": field.Name,
					"answer":     opt.Value,
				},
			})
		}
	}
	if !field.Required {
		buttons = append(buttons, feishu.Button{
			Text: "留空",
			Type: "default",
			Name: "elicitation_skip",
			Value: map[string]any{
				"action":     "elicitation_form.answer",
				"request_id": strings.TrimSpace(requestID),
				"field_name": field.Name,
				"answer":     "__skip__",
			},
		})
	}
	for start := 0; start < len(buttons); start += 3 {
		end := start + 3
		if end > len(buttons) {
			end = len(buttons)
		}
		appcards.AppendMarkdownBodyCardElement(card, appcards.BuildMarkdownBodyCardActionElement(buttons[start:end]))
	}
	appcards.AppendMarkdownBodyCardElement(card, appcards.BuildMarkdownBodyCardActionElement([]feishu.Button{{
		Text:  "取消",
		Type:  "default",
		Name:  "elicitation_form_cancel",
		Value: map[string]any{"action": "pending_form.cancel", "request_id": strings.TrimSpace(requestID)},
	}}))
	return card
}

func renderElicitationConfirmCard(requestID string, payload ElicitationFormPayload, attentionUserID string) map[string]any {
	card := appcards.NewMarkdownBodyCard("需要补充表单", "orange")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": apputil.PrependAttentionMentionMarkdown(renderElicitationConfirmIntro(payload), attentionUserID),
	})
	appcards.AppendMarkdownBodyCardElement(card, appcards.BuildMarkdownBodyCardActionElement([]feishu.Button{
		{
			Text: "允许",
			Type: "primary",
			Name: "elicitation_confirm_accept",
			Value: map[string]any{
				"action":             "elicitation_form.answer",
				"request_id":         strings.TrimSpace(requestID),
				"elicitation_action": "accept",
			},
		},
		{
			Text: "拒绝",
			Type: "danger",
			Name: "elicitation_confirm_decline",
			Value: map[string]any{
				"action":             "elicitation_form.answer",
				"request_id":         strings.TrimSpace(requestID),
				"elicitation_action": "decline",
			},
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "elicitation_form_cancel",
			Value: map[string]any{"action": "pending_form.cancel", "request_id": strings.TrimSpace(requestID)},
		},
	}))
	return card
}

func renderElicitationTextFallbackCard(payload ElicitationFormPayload, attentionUserID, reason, requestID string) map[string]any {
	card := appcards.NewMarkdownBodyCard("需要补充表单", "orange")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag": "markdown",
		"content": apputil.PrependAttentionMentionMarkdown(
			RenderElicitationTextFallbackBody(payload, reason),
			attentionUserID,
		),
	})
	appcards.AppendMarkdownBodyCardElement(card, appcards.BuildMarkdownBodyCardActionElement([]feishu.Button{{
		Text:  "取消",
		Type:  "default",
		Name:  "elicitation_form_cancel",
		Value: map[string]any{"action": "pending_form.cancel", "request_id": strings.TrimSpace(requestID)},
	}}))
	return card
}

func renderElicitationInlineIntro(payload ElicitationFormPayload, spec ElicitationFormSpec) string {
	lines := []string{strings.TrimSpace(payload.Message), "", "请在下方表单中填写。若卡片交互不可用，也可直接回复 `field_name: value`。"}
	if len(spec.Fields) == 1 {
		lines = append(lines, "", "字段数: 1")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderElicitationDecisionIntro(payload ElicitationFormPayload, field ElicitationField) string {
	lines := []string{
		strings.TrimSpace(payload.Message),
		"",
		"请直接点击下方按钮完成选择。",
		"",
		renderElicitationFieldMarkdown(field, FormDrafts{}),
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderElicitationConfirmIntro(payload ElicitationFormPayload) string {
	lines := []string{
		strings.TrimSpace(payload.Message),
		"",
		"请直接点击下方按钮确认。",
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderElicitationFieldElements(field ElicitationField, drafts FormDrafts, requestID string) []map[string]any {
	elements := []map[string]any{{
		"tag":     "markdown",
		"content": renderElicitationFieldMarkdown(field, drafts),
	}}
	switch field.Kind {
	case ElicitationFieldString, ElicitationFieldNumber, ElicitationFieldInteger:
		elements = append(elements, buildElicitationTextInputElement(field, drafts))
	case ElicitationFieldBoolean, ElicitationFieldSingleSelect:
		elements = append(elements, buildElicitationSingleSelectElement(field, drafts))
	case ElicitationFieldMultiSelect:
		elements = append(elements, buildElicitationMultiSelectRows(field, drafts, requestID)...)
	}
	return elements
}

func renderElicitationFieldMarkdown(field ElicitationField, drafts FormDrafts) string {
	title := strings.TrimSpace(field.Title)
	if title == "" {
		title = field.Name
	}
	lines := []string{"**" + title + RequiredMarker(field.Required) + "**", "`" + strings.TrimSpace(field.Name) + "`"}
	if desc := strings.TrimSpace(field.Description); desc != "" {
		lines = append(lines, desc)
	}
	if details := elicitationFieldDetailLines(field); len(details) > 0 {
		lines = append(lines, details...)
	}
	if current := elicitationFieldCurrentSummary(field, drafts); current != "" {
		switch field.Kind {
		case ElicitationFieldMultiSelect:
			lines = append(lines, "当前已选: "+current)
		default:
			lines = append(lines, "当前值: "+current)
		}
	}
	return strings.Join(lines, "\n")
}

func elicitationFieldDetailLines(field ElicitationField) []string {
	lines := make([]string, 0, 4)
	switch field.Kind {
	case ElicitationFieldBoolean:
		lines = append(lines, "单选题: 是 / 否")
	case ElicitationFieldSingleSelect:
		lines = append(lines, "单选题")
		lines = append(lines, "可选值: "+strings.Join(elicitationOptionLabels(field.Options), ", "))
	case ElicitationFieldMultiSelect:
		lines = append(lines, "多选题")
		lines = append(lines, "可选值: "+strings.Join(elicitationOptionLabels(field.Options), ", "))
	case ElicitationFieldString:
		if field.Format != "" {
			lines = append(lines, "格式: "+field.Format)
		}
		if constraint := elicitationStringConstraintText(field); constraint != "" {
			lines = append(lines, constraint)
		}
	case ElicitationFieldNumber, ElicitationFieldInteger:
		if constraint := elicitationNumberConstraintText(field); constraint != "" {
			lines = append(lines, constraint)
		}
	}
	if field.Kind == ElicitationFieldMultiSelect {
		if constraint := elicitationMultiConstraintText(field); constraint != "" {
			lines = append(lines, constraint)
		}
	}
	if field.HasDefault {
		if def := elicitationFieldDefaultSummary(field); def != "" {
			lines = append(lines, "默认值: "+def)
		}
	}
	return lines
}

func buildElicitationTextInputElement(field ElicitationField, drafts FormDrafts) map[string]any {
	input := map[string]any{
		"tag":         "input",
		"name":        field.Name,
		"required":    field.Required,
		"placeholder": map[string]any{"tag": "plain_text", "content": elicitationInputPlaceholder(field)},
	}
	if value, ok := elicitationDraftStringValue(drafts, field.Name); ok {
		input["default_value"] = value
	} else if field.HasDefault && field.DefaultText != "" {
		input["default_value"] = field.DefaultText
	}
	return input
}

func buildElicitationSingleSelectElement(field ElicitationField, drafts FormDrafts) map[string]any {
	options := make([]appcards.SelectStaticOption, 0, len(field.Options))
	switch field.Kind {
	case ElicitationFieldBoolean:
		options = append(options,
			appcards.SelectStaticOption{Text: "是", Value: "true"},
			appcards.SelectStaticOption{Text: "否", Value: "false"},
		)
	default:
		for _, opt := range field.Options {
			options = append(options, appcards.SelectStaticOption{Text: opt.Label, Value: opt.Value})
		}
	}
	initial := ""
	if value, ok := elicitationDraftStringValue(drafts, field.Name); ok {
		initial = strings.TrimSpace(value)
	} else if field.HasDefault {
		initial = strings.TrimSpace(field.DefaultText)
	}
	return appcards.BuildFormSelectStaticElement(field.Name, elicitationInputPlaceholder(field), options, initial)
}

func buildElicitationMultiSelectRows(field ElicitationField, drafts FormDrafts, requestID string) []map[string]any {
	selected := elicitationFieldCurrentMultiValues(field, drafts)
	selectedSet := map[string]struct{}{}
	for _, value := range selected {
		selectedSet[strings.TrimSpace(value)] = struct{}{}
	}
	buttons := make([]feishu.Button, 0, len(field.Options))
	for idx, opt := range field.Options {
		text := "[ ] " + opt.Label
		buttonType := "default"
		if _, ok := selectedSet[opt.Value]; ok {
			text = "[x] " + opt.Label
			buttonType = "primary"
		}
		buttons = append(buttons, feishu.Button{
			Text: text,
			Type: buttonType,
			Name: fmt.Sprintf("elicitation_toggle_%s_%d", strings.TrimSpace(field.Name), idx),
			Value: map[string]any{
				"action":       "elicitation_form.toggle_multi",
				"request_id":   strings.TrimSpace(requestID),
				"field_name":   field.Name,
				"option_value": opt.Value,
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

func elicitationInputPlaceholder(field ElicitationField) string {
	title := apputil.FirstNonEmpty(strings.TrimSpace(field.Title), strings.TrimSpace(field.Name), "请输入值")
	switch field.Kind {
	case ElicitationFieldBoolean:
		return title + " | 请选择是或否"
	case ElicitationFieldSingleSelect:
		return title + " | 请选择一个选项"
	case ElicitationFieldMultiSelect:
		return title + " | 点击按钮切换多选"
	case ElicitationFieldInteger:
		if constraint := elicitationNumberConstraintText(field); constraint != "" {
			return title + " | " + constraint
		}
		return title + " | 请输入整数"
	case ElicitationFieldNumber:
		if constraint := elicitationNumberConstraintText(field); constraint != "" {
			return title + " | " + constraint
		}
		return title + " | 请输入数字"
	case ElicitationFieldString:
		switch field.Format {
		case "email":
			return title + " | 请输入邮箱"
		case "uri":
			return title + " | 请输入 URL / URI"
		case "date":
			return title + " | YYYY-MM-DD"
		case "date-time":
			return title + " | RFC3339 日期时间"
		}
		if constraint := elicitationStringConstraintText(field); constraint != "" {
			return title + " | " + constraint
		}
	}
	return title
}

func elicitationFieldCurrentSummary(field ElicitationField, drafts FormDrafts) string {
	switch field.Kind {
	case ElicitationFieldMultiSelect:
		values := elicitationFieldCurrentMultiValues(field, drafts)
		if len(values) == 0 {
			return ""
		}
		return summarizeElicitationOptions(field, values)
	default:
		raw, ok := elicitationDraftStringValue(drafts, field.Name)
		if !ok && field.HasDefault {
			raw = field.DefaultText
			ok = raw != "" || field.Kind == ElicitationFieldBoolean
		}
		if !ok || strings.TrimSpace(raw) == "" {
			return ""
		}
		switch field.Kind {
		case ElicitationFieldBoolean:
			if value, _, include, err := normalizeElicitationTextValue(field, raw, true); err == nil && include {
				if b, ok := value.(bool); ok {
					return strconv.FormatBool(b)
				}
			}
		case ElicitationFieldSingleSelect:
			if value, _, include, err := normalizeElicitationTextValue(field, raw, true); err == nil && include {
				if s, ok := value.(string); ok {
					return summarizeElicitationOptions(field, []string{s})
				}
			}
		default:
			return raw
		}
	}
	return ""
}

func elicitationFieldCurrentMultiValues(field ElicitationField, drafts FormDrafts) []string {
	if values, ok := elicitationDraftMultiValues(drafts, field.Name); ok {
		return values
	}
	if len(field.DefaultMulti) > 0 {
		return append([]string(nil), field.DefaultMulti...)
	}
	return nil
}

func elicitationDraftValue(drafts FormDrafts, field ElicitationField) (string, bool) {
	if field.Kind == ElicitationFieldMultiSelect {
		return "", false
	}
	if value, ok := elicitationDraftStringValue(drafts, field.Name); ok {
		return value, true
	}
	if field.HasDefault {
		return field.DefaultText, true
	}
	return "", false
}

func elicitationMultiDraftValues(drafts FormDrafts, field ElicitationField) ([]string, bool) {
	if values, ok := elicitationDraftMultiValues(drafts, field.Name); ok {
		return values, true
	}
	if len(field.DefaultMulti) > 0 {
		return append([]string(nil), field.DefaultMulti...), true
	}
	return nil, false
}

func elicitationDraftStringValue(drafts FormDrafts, key string) (string, bool) {
	if drafts.Values == nil {
		return "", false
	}
	value, ok := drafts.Values[strings.TrimSpace(key)]
	return strings.TrimSpace(value), ok
}

func elicitationDraftMultiValues(drafts FormDrafts, key string) ([]string, bool) {
	if drafts.Multi == nil {
		return nil, false
	}
	if _, ok := drafts.Multi[strings.TrimSpace(key)]; !ok {
		return nil, false
	}
	return ToolUserInputMultiDraftValues(drafts, key), true
}

func parseElicitationField(name string, raw map[string]any, required bool) (ElicitationField, error) {
	field := ElicitationField{
		Name:        strings.TrimSpace(name),
		Title:       strings.TrimSpace(DisplayFieldTitle(name, raw)),
		Description: strings.TrimSpace(StringField(raw, "description")),
		Required:    required,
	}
	switch {
	case raw == nil:
		return field, fmt.Errorf("field schema is missing")
	case raw["oneOf"] != nil:
		field.Kind = ElicitationFieldSingleSelect
		options, err := parseElicitationOneOfOptions(raw["oneOf"])
		if err != nil {
			return field, err
		}
		field.Options = options
		if value, ok, err := parseStringDefault(raw["default"]); err != nil {
			return field, err
		} else if ok {
			if _, err := matchElicitationOption(field.Options, value); err != nil {
				return field, fmt.Errorf("invalid default %q", value)
			}
			field.DefaultText = value
			field.HasDefault = true
		}
		return field, nil
	case raw["enum"] != nil:
		field.Kind = ElicitationFieldSingleSelect
		options, err := parseElicitationEnumOptions(raw["enum"], raw["enumNames"])
		if err != nil {
			return field, err
		}
		field.Options = options
		if value, ok, err := parseStringDefault(raw["default"]); err != nil {
			return field, err
		} else if ok {
			if _, err := matchElicitationOption(field.Options, value); err != nil {
				return field, fmt.Errorf("invalid default %q", value)
			}
			field.DefaultText = value
			field.HasDefault = true
		}
		return field, nil
	}

	switch strings.TrimSpace(FieldType(raw)) {
	case "", "string":
		field.Kind = ElicitationFieldString
		field.Format = strings.TrimSpace(StringField(raw, "format"))
		if field.Format != "" && field.Format != "email" && field.Format != "uri" && field.Format != "date" && field.Format != "date-time" {
			return field, fmt.Errorf("unsupported string format %q", field.Format)
		}
		field.MinLength = parseOptionalInt(raw["minLength"])
		field.MaxLength = parseOptionalInt(raw["maxLength"])
		if value, ok, err := parseStringDefault(raw["default"]); err != nil {
			return field, err
		} else if ok {
			field.DefaultText = value
			field.HasDefault = true
		}
	case "number":
		field.Kind = ElicitationFieldNumber
		field.Minimum = parseOptionalFloat(raw["minimum"])
		field.Maximum = parseOptionalFloat(raw["maximum"])
		if value, ok, err := parseNumericDefault(raw["default"]); err != nil {
			return field, err
		} else if ok {
			field.DefaultText = value
			field.HasDefault = true
		}
	case "integer":
		field.Kind = ElicitationFieldInteger
		field.Minimum = parseOptionalFloat(raw["minimum"])
		field.Maximum = parseOptionalFloat(raw["maximum"])
		if value, ok, err := parseIntegerDefault(raw["default"]); err != nil {
			return field, err
		} else if ok {
			field.DefaultText = value
			field.HasDefault = true
		}
	case "boolean":
		field.Kind = ElicitationFieldBoolean
		if value, ok, err := parseBooleanDefault(raw["default"]); err != nil {
			return field, err
		} else if ok {
			field.DefaultText = value
			field.HasDefault = true
		}
	case "array":
		field.Kind = ElicitationFieldMultiSelect
		items, _ := raw["items"].(map[string]any)
		options, err := parseElicitationArrayOptions(items)
		if err != nil {
			return field, err
		}
		field.Options = options
		field.MinItems = parseOptionalInt(raw["minItems"])
		field.MaxItems = parseOptionalInt(raw["maxItems"])
		if values, ok, err := parseStringListDefault(raw["default"]); err != nil {
			return field, err
		} else if ok {
			normalized := make([]string, 0, len(values))
			for _, value := range values {
				matched, err := matchElicitationOption(field.Options, value)
				if err != nil {
					return field, fmt.Errorf("invalid default %q", value)
				}
				normalized = append(normalized, matched.Value)
			}
			field.DefaultMulti = UniqueToolUserInputParts(normalized)
			field.HasDefault = len(field.DefaultMulti) > 0
		}
	default:
		return field, fmt.Errorf("unsupported field type %q", FieldType(raw))
	}
	return field, nil
}

func parseElicitationEnumOptions(rawEnum, rawNames any) ([]ElicitationOption, error) {
	values, _ := rawEnum.([]any)
	if len(values) == 0 {
		return nil, fmt.Errorf("enum must define at least one option")
	}
	names, _ := rawNames.([]any)
	options := make([]ElicitationOption, 0, len(values))
	seen := map[string]struct{}{}
	for idx, raw := range values {
		value, _ := raw.(string)
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("enum contains empty option")
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("enum contains duplicate option %q", value)
		}
		seen[key] = struct{}{}
		label := value
		if idx < len(names) {
			if named, ok := names[idx].(string); ok && strings.TrimSpace(named) != "" {
				label = strings.TrimSpace(named)
			}
		}
		options = append(options, ElicitationOption{Value: value, Label: label})
	}
	return options, nil
}

func parseElicitationOneOfOptions(raw any) ([]ElicitationOption, error) {
	values, _ := raw.([]any)
	if len(values) == 0 {
		return nil, fmt.Errorf("oneOf must define at least one option")
	}
	options := make([]ElicitationOption, 0, len(values))
	seen := map[string]struct{}{}
	for _, item := range values {
		entry, _ := item.(map[string]any)
		value := strings.TrimSpace(StringField(entry, "const"))
		if value == "" {
			return nil, fmt.Errorf("oneOf option is missing const")
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("oneOf contains duplicate option %q", value)
		}
		seen[key] = struct{}{}
		label := strings.TrimSpace(StringField(entry, "title"))
		if label == "" {
			label = value
		}
		options = append(options, ElicitationOption{Value: value, Label: label})
	}
	return options, nil
}

func parseElicitationArrayOptions(raw map[string]any) ([]ElicitationOption, error) {
	if raw == nil {
		return nil, fmt.Errorf("array field must define items")
	}
	switch {
	case raw["enum"] != nil:
		return parseElicitationEnumOptions(raw["enum"], nil)
	case raw["anyOf"] != nil:
		return parseElicitationOneOfOptions(raw["anyOf"])
	default:
		return nil, fmt.Errorf("unsupported array items schema")
	}
}

func parseStringDefault(raw any) (string, bool, error) {
	switch value := raw.(type) {
	case nil:
		return "", false, nil
	case string:
		return strings.TrimSpace(value), true, nil
	default:
		return "", false, fmt.Errorf("default must be a string")
	}
}

func parseBooleanDefault(raw any) (string, bool, error) {
	switch value := raw.(type) {
	case nil:
		return "", false, nil
	case bool:
		return strconv.FormatBool(value), true, nil
	default:
		return "", false, fmt.Errorf("default must be a boolean")
	}
}

func parseNumericDefault(raw any) (string, bool, error) {
	switch value := raw.(type) {
	case nil:
		return "", false, nil
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true, nil
	case int:
		return strconv.Itoa(value), true, nil
	default:
		return "", false, fmt.Errorf("default must be numeric")
	}
}

func parseIntegerDefault(raw any) (string, bool, error) {
	switch value := raw.(type) {
	case nil:
		return "", false, nil
	case float64:
		if math.Trunc(value) != value {
			return "", false, fmt.Errorf("default must be an integer")
		}
		return strconv.FormatInt(int64(value), 10), true, nil
	case int:
		return strconv.Itoa(value), true, nil
	default:
		return "", false, fmt.Errorf("default must be an integer")
	}
}

func parseStringListDefault(raw any) ([]string, bool, error) {
	switch value := raw.(type) {
	case nil:
		return nil, false, nil
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, false, fmt.Errorf("default array must contain strings")
			}
			text = strings.TrimSpace(text)
			if text != "" {
				out = append(out, text)
			}
		}
		return out, true, nil
	case []string:
		out := make([]string, 0, len(value))
		for _, item := range value {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out, true, nil
	default:
		return nil, false, fmt.Errorf("default must be an array of strings")
	}
}

func parseOptionalInt(raw any) *int {
	switch value := raw.(type) {
	case float64:
		if math.Trunc(value) != value {
			return nil
		}
		out := int(value)
		return &out
	case int:
		out := value
		return &out
	}
	return nil
}

func parseOptionalFloat(raw any) *float64 {
	switch value := raw.(type) {
	case float64:
		out := value
		return &out
	case int:
		out := float64(value)
		return &out
	}
	return nil
}

func normalizeElicitationTextValue(field ElicitationField, raw string, provided bool) (any, string, bool, error) {
	raw = strings.TrimSpace(raw)
	if !provided || raw == "" {
		if field.Required {
			return nil, "", false, fmt.Errorf("answer is required")
		}
		return nil, "", false, nil
	}
	switch field.Kind {
	case ElicitationFieldString:
		if err := validateElicitationString(field, raw); err != nil {
			return nil, "", false, err
		}
		return raw, raw, true, nil
	case ElicitationFieldNumber:
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, "", false, fmt.Errorf("expect number")
		}
		if err := validateElicitationNumber(field, value); err != nil {
			return nil, "", false, err
		}
		return value, raw, true, nil
	case ElicitationFieldInteger:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, "", false, fmt.Errorf("expect integer")
		}
		if err := validateElicitationNumber(field, float64(value)); err != nil {
			return nil, "", false, err
		}
		return value, raw, true, nil
	case ElicitationFieldBoolean:
		value, err := ParseBool(raw)
		if err != nil {
			return nil, "", false, err
		}
		return value, strconv.FormatBool(value), true, nil
	case ElicitationFieldSingleSelect:
		matched, err := matchElicitationOption(field.Options, raw)
		if err != nil {
			return nil, "", false, err
		}
		return matched.Value, elicitationOptionSummary(matched), true, nil
	default:
		return nil, "", false, fmt.Errorf("unsupported field type %q", field.Kind)
	}
}

func normalizeElicitationMultiValue(field ElicitationField, raw []string, provided bool) ([]string, string, bool, error) {
	values := raw
	if !provided {
		values = nil
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		matched, err := matchElicitationOption(field.Options, value)
		if err != nil {
			return nil, "", false, err
		}
		normalized = append(normalized, matched.Value)
	}
	normalized = UniqueToolUserInputParts(normalized)
	if len(normalized) == 0 {
		if field.Required || (field.MinItems != nil && *field.MinItems > 0) {
			return nil, "", false, fmt.Errorf("select at least one option")
		}
		return nil, "", false, nil
	}
	if field.MinItems != nil && len(normalized) < *field.MinItems {
		return nil, "", false, fmt.Errorf("select at least %d option(s)", *field.MinItems)
	}
	if field.MaxItems != nil && len(normalized) > *field.MaxItems {
		return nil, "", false, fmt.Errorf("select at most %d option(s)", *field.MaxItems)
	}
	return normalized, summarizeElicitationOptions(field, normalized), true, nil
}

func validateElicitationString(field ElicitationField, raw string) error {
	length := utf8.RuneCountInString(raw)
	if field.MinLength != nil && length < *field.MinLength {
		return fmt.Errorf("must contain at least %d character(s)", *field.MinLength)
	}
	if field.MaxLength != nil && length > *field.MaxLength {
		return fmt.Errorf("must contain at most %d character(s)", *field.MaxLength)
	}
	switch field.Format {
	case "":
		return nil
	case "email":
		addr, err := mail.ParseAddress(raw)
		if err != nil || !strings.EqualFold(strings.TrimSpace(addr.Address), raw) {
			return fmt.Errorf("expect valid email")
		}
	case "uri":
		parsed, err := url.Parse(raw)
		if err != nil || strings.TrimSpace(parsed.Scheme) == "" {
			return fmt.Errorf("expect valid uri")
		}
	case "date":
		if _, err := time.Parse("2006-01-02", raw); err != nil {
			return fmt.Errorf("expect date in YYYY-MM-DD")
		}
	case "date-time":
		if _, err := time.Parse(time.RFC3339, raw); err != nil {
			return fmt.Errorf("expect RFC3339 date-time")
		}
	default:
		return fmt.Errorf("unsupported string format %q", field.Format)
	}
	return nil
}

func validateElicitationNumber(field ElicitationField, value float64) error {
	if field.Minimum != nil && value < *field.Minimum {
		return fmt.Errorf("must be greater than or equal to %s", formatConstraintNumber(*field.Minimum))
	}
	if field.Maximum != nil && value > *field.Maximum {
		return fmt.Errorf("must be less than or equal to %s", formatConstraintNumber(*field.Maximum))
	}
	return nil
}

func matchElicitationOption(options []ElicitationOption, raw string) (ElicitationOption, error) {
	raw = strings.TrimSpace(raw)
	for _, opt := range options {
		if strings.EqualFold(raw, strings.TrimSpace(opt.Value)) {
			return opt, nil
		}
		if strings.EqualFold(raw, strings.TrimSpace(opt.Label)) {
			return opt, nil
		}
	}
	return ElicitationOption{}, fmt.Errorf("unsupported option %q", raw)
}

func summarizeElicitationOptions(field ElicitationField, values []string) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		matched, err := matchElicitationOption(field.Options, value)
		if err != nil {
			parts = append(parts, strings.TrimSpace(value))
			continue
		}
		parts = append(parts, elicitationOptionSummary(matched))
	}
	return strings.Join(parts, ", ")
}

func elicitationOptionSummary(opt ElicitationOption) string {
	label := strings.TrimSpace(opt.Label)
	value := strings.TrimSpace(opt.Value)
	switch {
	case label == "":
		return value
	case strings.EqualFold(label, value):
		return value
	default:
		return label + " (`" + value + "`)"
	}
}

func elicitationOptionLabels(options []ElicitationOption) []string {
	out := make([]string, 0, len(options))
	for _, opt := range options {
		out = append(out, strings.TrimSpace(opt.Label))
	}
	return out
}

func elicitationFieldDefaultSummary(field ElicitationField) string {
	switch field.Kind {
	case ElicitationFieldMultiSelect:
		return summarizeElicitationOptions(field, field.DefaultMulti)
	case ElicitationFieldSingleSelect:
		return summarizeElicitationOptions(field, []string{field.DefaultText})
	default:
		return strings.TrimSpace(field.DefaultText)
	}
}

func elicitationStringConstraintText(field ElicitationField) string {
	switch {
	case field.MinLength != nil && field.MaxLength != nil:
		return fmt.Sprintf("长度: %d-%d 字符", *field.MinLength, *field.MaxLength)
	case field.MinLength != nil:
		return fmt.Sprintf("长度: 至少 %d 字符", *field.MinLength)
	case field.MaxLength != nil:
		return fmt.Sprintf("长度: 至多 %d 字符", *field.MaxLength)
	default:
		return ""
	}
}

func elicitationNumberConstraintText(field ElicitationField) string {
	switch {
	case field.Minimum != nil && field.Maximum != nil:
		return fmt.Sprintf("范围: %s - %s", formatConstraintNumber(*field.Minimum), formatConstraintNumber(*field.Maximum))
	case field.Minimum != nil:
		return fmt.Sprintf("范围: >= %s", formatConstraintNumber(*field.Minimum))
	case field.Maximum != nil:
		return fmt.Sprintf("范围: <= %s", formatConstraintNumber(*field.Maximum))
	default:
		return ""
	}
}

func elicitationMultiConstraintText(field ElicitationField) string {
	switch {
	case field.MinItems != nil && field.MaxItems != nil:
		return fmt.Sprintf("选择数量: %d-%d 项", *field.MinItems, *field.MaxItems)
	case field.MinItems != nil:
		return fmt.Sprintf("选择数量: 至少 %d 项", *field.MinItems)
	case field.MaxItems != nil:
		return fmt.Sprintf("选择数量: 至多 %d 项", *field.MaxItems)
	default:
		return ""
	}
}

func formatConstraintNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// RenderElicitationTextFallbackBody explains why a schema was downgraded to text input.
func RenderElicitationTextFallbackBody(payload ElicitationFormPayload, reason string) string {
	lines := []string{
		"该表单暂时无法渲染为交互式卡片，已回退为文本填写模式。",
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		lines = append(lines, "原因: "+reason)
	}
	lines = append(lines, "", RenderElicitationFormBody(payload))
	return strings.Join(lines, "\n")
}
