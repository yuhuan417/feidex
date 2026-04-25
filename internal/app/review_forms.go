package app

import (
	"context"
	appreview "feidex/internal/app/review"
	"fmt"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type reviewFormService struct {
	app *App
}
func newReviewFormService(app *App) reviewFormService {
	return reviewFormService{app: app}
}

func (s reviewFormService) renderReviewMenuCard(sessionKey string) map[string]any {
	bodyLines := []string{
		"在当前线程启动 inline review。",
		"",
		"- `未提交改动`: 直接审查当前工作树。",
		"- `对比分支`: 选择一个 base branch。",
		"- `审查 commit`: 从最近 100 个 commit 里选择。",
		"- `自定义审查`: 提供 free-form instructions。",
	}
	buttons := []feishu.Button{
		{
			Text:  commandLabel("审查未提交改动", "/review"),
			Type:  "default",
			Value: map[string]any{"action": "menu.review.uncommitted", "session_key": sessionKey},
		},
		{
			Text:  submenuCommandLabel("对比分支审查", "/review base"),
			Type:  "default",
			Value: map[string]any{"action": "menu.review.base", "session_key": sessionKey},
		},
		{
			Text:  submenuCommandLabel("审查单个 commit", "/review commit"),
			Type:  "default",
			Value: map[string]any{"action": "menu.review.commit", "session_key": sessionKey},
		},
		{
			Text:  submenuCommandLabel("自定义审查", "/review custom"),
			Type:  "default",
			Value: map[string]any{"action": "menu.review.custom", "session_key": sessionKey},
		},
		{
			Text:  "返回上一级",
			Type:  "default",
			Value: map[string]any{"action": "menu.tools", "session_key": sessionKey},
		},
	}
	return s.app.feishu.SimpleStatusCard("代码审查", "blue", menuCardBody("menu.review", strings.Join(bodyLines, "\n")), buttons)
}

func (s reviewFormService) beginReviewForm(msg *feishu.InboundMessage, mode string) error {
	sessionKey, _, ws := newWorkspaceConfigService(s.app).currentWorkspaceForMessage(msg)
	if ws == nil {
		return fmt.Errorf("current workspace not found")
	}
	if strings.TrimSpace(mode) == "" {
		return fmt.Errorf("review form mode is required")
	}
	payload := reviewPendingPayload{Mode: strings.TrimSpace(mode)}
	switch payload.Mode {
	case reviewFormModeBase:
		options, err := newReviewGitService(s.app).listReviewBranches(ws.Cwd)
		if err != nil {
			return err
		}
		if len(options) == 0 {
			return fmt.Errorf("当前仓库没有可选 branch")
		}
		payload.Branch = options[0].Name
	case reviewFormModeCommit:
		options, err := newReviewGitService(s.app).listReviewCommits(ws.Cwd, 100)
		if err != nil {
			return err
		}
		if len(options) == 0 {
			return fmt.Errorf("当前仓库没有可选 commit")
		}
		payload.CommitSHA = options[0].SHA
		payload.CommitTitle = options[0].Subject
	case reviewFormModeCustom:
	default:
		return fmt.Errorf("unsupported review form mode %q", payload.Mode)
	}
	appState := appState(s.app)
	requestID, err := appState.nextLocalID("review")
	if err != nil {
		return err
	}
	card, err := newReviewFormService(s.app).renderReviewFormCard(sessionKey, requestID, payload)
	if err != nil {
		return err
	}
	msgID, err := s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	if err != nil {
		return err
	}
	return appState.savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        pendingKindReview,
		SessionKey:  sessionKey,
		OwnerUserID: msg.UserID,
		FeishuMsgID: msgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
}

func (s reviewFormService) renderReviewFormCard(sessionKey, requestID string, payload reviewPendingPayload) (map[string]any, error) {
	switch strings.TrimSpace(payload.Mode) {
	case reviewFormModeBase:
		return newReviewFormService(s.app).renderReviewBaseCard(sessionKey, requestID, payload)
	case reviewFormModeCommit:
		return newReviewFormService(s.app).renderReviewCommitCard(sessionKey, requestID, payload)
	case reviewFormModeCustom:
		return newReviewFormService(s.app).renderReviewCustomCard(sessionKey, requestID, payload), nil
	default:
		return nil, fmt.Errorf("unsupported review form mode %q", payload.Mode)
	}
}

func (s reviewFormService) renderReviewBaseCard(sessionKey, requestID string, payload reviewPendingPayload) (map[string]any, error) {
	ws := newReviewGitService(s.app).workspaceForSessionKey(sessionKey)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	options, err := newReviewGitService(s.app).listReviewBranches(ws.Cwd)
	if err != nil {
		return nil, err
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("当前仓库没有可选 branch")
	}
	selected := strings.TrimSpace(payload.Branch)
	if selected == "" || !appreview.BranchExists(options, selected) {
		selected = options[0].Name
	}
	selectedLabel := selected
	for _, option := range options {
		if option.Name == selected {
			selectedLabel = appreview.BranchOptionLabel(option)
			break
		}
	}
	card := newMarkdownBodyCard("选择 Base Branch", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag": "markdown",
		"content": menuCardBody("menu.review",
			"选择一个 base branch，然后开始 review。\n\n当前选择: `"+inlineCodeText(selected)+"`\n"+selectedLabel),
	})
	selectOptions := make([]selectStaticOption, 0, len(options))
	for _, option := range options {
		selectOptions = append(selectOptions, selectStaticOption{
			Text:  appreview.BranchOptionLabel(option),
			Value: option.Name,
		})
	}
	appendMarkdownBodyCardElement(card, buildSelectStaticElement(
		"review_branch",
		"选择 base branch",
		map[string]any{"action": "review.base.select", "request_id": requestID},
		selectOptions,
		selected,
	))
	for _, row := range buildMarkdownBodyCardActionElements([]feishu.Button{
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
	}) {
		appendMarkdownBodyCardElement(card, row)
	}
	return card, nil
}

func (s reviewFormService) renderReviewCommitCard(sessionKey, requestID string, payload reviewPendingPayload) (map[string]any, error) {
	ws := newReviewGitService(s.app).workspaceForSessionKey(sessionKey)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	options, err := newReviewGitService(s.app).listReviewCommits(ws.Cwd, 100)
	if err != nil {
		return nil, err
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("当前仓库没有可选 commit")
	}
	selected := strings.TrimSpace(payload.CommitSHA)
	if selected == "" || !appreview.CommitExists(options, selected) {
		selected = options[0].SHA
	}
	selectedLabel := selected
	for _, option := range options {
		if option.SHA == selected {
			selectedLabel = appreview.CommitOptionLabel(option)
			break
		}
	}
	card := newMarkdownBodyCard("选择 Commit", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag": "markdown",
		"content": menuCardBody("menu.review",
			"从最近 100 个 commit 中选择一个 target。\n\n当前选择: `"+inlineCodeText(appreview.ShortCommitSHA(selected))+"`\n"+selectedLabel),
	})
	selectOptions := make([]selectStaticOption, 0, len(options))
	for _, option := range options {
		selectOptions = append(selectOptions, selectStaticOption{
			Text:  appreview.CommitOptionLabel(option),
			Value: option.SHA,
		})
	}
	appendMarkdownBodyCardElement(card, buildSelectStaticElement(
		"review_commit",
		"选择 commit",
		map[string]any{"action": "review.commit.select", "request_id": requestID},
		selectOptions,
		selected,
	))
	for _, row := range buildMarkdownBodyCardActionElements([]feishu.Button{
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
	}) {
		appendMarkdownBodyCardElement(card, row)
	}
	return card, nil
}

func (s reviewFormService) renderReviewCustomCard(sessionKey, requestID string, payload reviewPendingPayload) map[string]any {
	card := newMarkdownBodyCard("自定义审查", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag":     "markdown",
		"content": menuCardBody("menu.review", "填写 review instructions，然后开始 review。"),
	})
	instructionsInput := map[string]any{
		"tag":         "input",
		"name":        "instructions",
		"required":    true,
		"placeholder": map[string]any{"tag": "plain_text", "content": "例如：关注回归风险、边界条件和测试缺口"},
	}
	if value := strings.TrimSpace(payload.Instructions); value != "" {
		instructionsInput["default_value"] = value
	}
	formRows := buildMarkdownBodyCardActionElements([]feishu.Button{
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
	})
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
	appendMarkdownBodyCardElement(card, map[string]any{
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

func (s reviewFormService) completeReviewBaseSelect(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, _, errResp := newReviewFormService(s.app).reviewPendingForAction(action, reviewFormModeBase)
	if errResp != nil {
		return errResp, nil
	}
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return newReviewFormService(s.app).completeReviewBaseSelectSync(action)
	}
	return completeAsyncRenderedCardAction(s.app,
		action,
		pending.SessionKey,
		"正在刷新 review 选项",
		renderReviewPreparingCard(s.app, pending.SessionKey, "正在刷新 base branch 选择，请稍候。\n\n这张卡片会自动刷新。"),
		func() (*callback.CardActionTriggerResponse, error) {
			return newReviewFormService(s.app).completeReviewBaseSelectSync(action)
		},
		func(sessionKey, errText string) map[string]any {
			return renderReviewFailureCard(s.app, sessionKey, errText, "")
		},
		"review base select patch failed",
	)
}

func (s reviewFormService) completeReviewBaseSelectSync(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, payload, errResp := newReviewFormService(s.app).reviewPendingForAction(action, reviewFormModeBase)
	if errResp != nil {
		return errResp, nil
	}
	selected := strings.TrimSpace(action.Option)
	if selected == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效 branch"}}, nil
	}
	payload.Branch = selected
	_ = appState(s.app).updatePending(pending.ID, func(req *state.PendingRequest) {
		req.PayloadJSON = mustJSON(payload)
	})
	card, err := newReviewFormService(s.app).renderReviewBaseCard(pending.SessionKey, pending.ID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Card: rawCard(card)}, nil
}

func (s reviewFormService) completeReviewCommitSelect(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, _, errResp := newReviewFormService(s.app).reviewPendingForAction(action, reviewFormModeCommit)
	if errResp != nil {
		return errResp, nil
	}
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return newReviewFormService(s.app).completeReviewCommitSelectSync(action)
	}
	return completeAsyncRenderedCardAction(s.app,
		action,
		pending.SessionKey,
		"正在刷新 review 选项",
		renderReviewPreparingCard(s.app, pending.SessionKey, "正在刷新 commit 选择，请稍候。\n\n这张卡片会自动刷新。"),
		func() (*callback.CardActionTriggerResponse, error) {
			return newReviewFormService(s.app).completeReviewCommitSelectSync(action)
		},
		func(sessionKey, errText string) map[string]any {
			return renderReviewFailureCard(s.app, sessionKey, errText, "")
		},
		"review commit select patch failed",
	)
}

func (s reviewFormService) completeReviewCommitSelectSync(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, payload, errResp := newReviewFormService(s.app).reviewPendingForAction(action, reviewFormModeCommit)
	if errResp != nil {
		return errResp, nil
	}
	selected := strings.TrimSpace(action.Option)
	if selected == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效 commit"}}, nil
	}
	payload.CommitSHA = selected
	if ws := newReviewGitService(s.app).workspaceForSessionKey(pending.SessionKey); ws != nil {
		options, err := newReviewGitService(s.app).listReviewCommits(ws.Cwd, 100)
		if err == nil {
			for _, option := range options {
				if option.SHA == selected {
					payload.CommitTitle = option.Subject
					break
				}
			}
		}
	}
	_ = appState(s.app).updatePending(pending.ID, func(req *state.PendingRequest) {
		req.PayloadJSON = mustJSON(payload)
	})
	card, err := newReviewFormService(s.app).renderReviewCommitCard(pending.SessionKey, pending.ID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Card: rawCard(card)}, nil
}

func (s reviewFormService) completeReviewFormSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := appState(s.app)
	requestID := actionStringValue(action, "request_id")
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != pendingKindReview {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "review 请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个 review 请求"}}, nil
	}
	payload := reviewPendingPayloadFromPending(pending)
	if action == nil || strings.TrimSpace(action.MessageID) == "" || strings.TrimSpace(payload.Mode) == reviewFormModeCustom {
		return newReviewFormService(s.app).completeReviewFormSubmitSync(action)
	}
	return completeAsyncRenderedCardAction(s.app,
		action,
		pending.SessionKey,
		"正在启动 review",
		renderReviewPreparingCard(s.app, pending.SessionKey, "正在启动 review，请稍候。\n\n这张卡片会自动刷新。"),
		func() (*callback.CardActionTriggerResponse, error) {
			return newReviewFormService(s.app).completeReviewFormSubmitSync(action)
		},
		func(sessionKey, errText string) map[string]any {
			return renderReviewFailureCard(s.app, sessionKey, errText, "")
		},
		"review submit patch failed",
	)
}

func (s reviewFormService) completeReviewFormSubmitSync(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := appState(s.app)
	requestID := actionStringValue(action, "request_id")
	pending := appState.pending(requestID)
	if pending == nil || pending.Kind != pendingKindReview {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "review 请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个 review 请求"}}, nil
	}
	payload := reviewPendingPayloadFromPending(pending)
	payload = mergeReviewCustomFormValues(payload, action.FormValue)

	var target appreview.TargetSpec
	switch strings.TrimSpace(payload.Mode) {
	case reviewFormModeBase:
		target = appreview.TargetSpec{Type: appreview.TargetBaseBranch, Branch: strings.TrimSpace(payload.Branch)}
	case reviewFormModeCommit:
		target = appreview.TargetSpec{Type: appreview.TargetCommit, CommitSHA: strings.TrimSpace(payload.CommitSHA), CommitTitle: strings.TrimSpace(payload.CommitTitle)}
	case reviewFormModeCustom:
		target = appreview.TargetSpec{Type: appreview.TargetCustom, Instructions: strings.TrimSpace(payload.Instructions)}
	default:
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未知 review 表单"}}, nil
	}
	msg := commandMessageFromAction(s.app, action, pending.SessionKey, "/review")
	confirmation, err := startInlineReview(s.app, msg, target)
	if err != nil {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
		card, renderErr := newReviewFormService(s.app).renderReviewFormCard(pending.SessionKey, requestID, payload)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(card),
		}, nil
	}
	_ = appState.updatePending(requestID, func(req *state.PendingRequest) {
		req.Status = "resolved"
		req.PayloadJSON = mustJSON(payload)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已启动 review"},
		Card:  rawCard(s.app.feishu.SimpleStatusCard("Review 已启动", "blue", confirmation, nil)),
	}, nil
}

func (s reviewFormService) reviewPendingForAction(action *feishu.CardAction, mode string) (*state.PendingRequest, reviewPendingPayload, *callback.CardActionTriggerResponse) {
	requestID := actionStringValue(action, "request_id")
	pending := appState(s.app).pending(requestID)
	if pending == nil || pending.Kind != pendingKindReview {
		return nil, reviewPendingPayload{}, &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "review 请求已过期"}}
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return nil, reviewPendingPayload{}, &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个 review 请求"}}
	}
	payload := reviewPendingPayloadFromPending(pending)
	if strings.TrimSpace(payload.Mode) != strings.TrimSpace(mode) {
		return nil, reviewPendingPayload{}, &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "review 请求类型不匹配"}}
	}
	return pending, payload, nil
}
