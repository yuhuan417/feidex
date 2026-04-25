package review

import (
	"strings"

	appcards "feidex/internal/app/cards"
	"feidex/internal/feishu"
)

// RenderBaseBranchFormCard builds the branch selection form card.
// bodyText is the pre-formatted markdown body (caller should run through
// menuCardBody or equivalent before passing).
func RenderBaseBranchFormCard(sessionKey, requestID, bodyText, selectedLabel string, options []BranchOption, selected string) map[string]any {
	card := appcards.NewMarkdownBodyCard("选择 Base Branch", "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": bodyText,
	})
	selectOptions := make([]appcards.SelectStaticOption, 0, len(options))
	for _, option := range options {
		selectOptions = append(selectOptions, appcards.SelectStaticOption{
			Text:  BranchOptionLabel(option),
			Value: option.Name,
		})
	}
	appcards.AppendMarkdownBodyCardElement(card, appcards.BuildSelectStaticElement(
		"review_branch",
		"选择 base branch",
		map[string]any{"action": "review.base.select", "request_id": requestID},
		selectOptions,
		selected,
	))
	for _, row := range appcards.BuildMarkdownBodyCardActionElements(ReviewFormButtons(requestID)) {
		appcards.AppendMarkdownBodyCardElement(card, row)
	}
	return card
}

// RenderCommitFormCard builds the commit selection form card.
// bodyText is the pre-formatted markdown body.
func RenderCommitFormCard(sessionKey, requestID, bodyText, selectedLabel string, options []CommitOption, selected string) map[string]any {
	card := appcards.NewMarkdownBodyCard("选择 Commit", "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": bodyText,
	})
	selectOptions := make([]appcards.SelectStaticOption, 0, len(options))
	for _, option := range options {
		selectOptions = append(selectOptions, appcards.SelectStaticOption{
			Text:  CommitOptionLabel(option),
			Value: option.SHA,
		})
	}
	appcards.AppendMarkdownBodyCardElement(card, appcards.BuildSelectStaticElement(
		"review_commit",
		"选择 commit",
		map[string]any{"action": "review.commit.select", "request_id": requestID},
		selectOptions,
		selected,
	))
	for _, row := range appcards.BuildMarkdownBodyCardActionElements(ReviewFormButtons(requestID)) {
		appcards.AppendMarkdownBodyCardElement(card, row)
	}
	return card
}

// RenderCustomFormCard builds the custom review instructions form card.
// bodyText is the pre-formatted markdown body.
func RenderCustomFormCard(sessionKey, requestID, bodyText, instructions string) map[string]any {
	card := appcards.NewMarkdownBodyCard("自定义审查", "blue")
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": bodyText,
	})
	instructionsInput := map[string]any{
		"tag":         "input",
		"name":        "instructions",
		"required":    true,
		"placeholder": map[string]any{"tag": "plain_text", "content": "例如：关注回归风险、边界条件和测试缺口"},
	}
	if value := strings.TrimSpace(instructions); value != "" {
		instructionsInput["default_value"] = value
	}
	formRows := appcards.BuildMarkdownBodyCardActionElements(ReviewCustomFormButtons(requestID))
	for idx, row := range formRows {
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
	appcards.AppendMarkdownBodyCardElement(card, map[string]any{
		"tag":                "form",
		"name":               "review_custom_form",
		"direction":          "vertical",
		"horizontal_spacing": "8px",
		"vertical_spacing":   "8px",
		"elements": append([]map[string]any{
			instructionsInput,
		}, formRows...),
	})
	return card
}

// ReviewFormButtons returns the standard submit/cancel buttons for review forms.
func ReviewFormButtons(requestID string) []feishu.Button {
	return []feishu.Button{
		{
			Text:  "开始 review",
			Type:  "primary",
			Value: map[string]any{"action": "review.form.submit", "request_id": requestID},
		},
		{
			Text:  "取消",
			Type:  "default",
			Value: map[string]any{"action": "pending_form.cancel", "request_id": requestID},
		},
	}
}

// ReviewCustomFormButtons returns the submit/cancel buttons for the custom
// review form (with Name fields for form submission).
func ReviewCustomFormButtons(requestID string) []feishu.Button {
	return []feishu.Button{
		{
			Text:  "开始 review",
			Type:  "primary",
			Name:  "review_custom_submit",
			Value: map[string]any{"action": "review.form.submit", "request_id": requestID},
		},
		{
			Text:  "取消",
			Type:  "default",
			Name:  "review_custom_cancel",
			Value: map[string]any{"action": "pending_form.cancel", "request_id": requestID},
		},
	}
}
