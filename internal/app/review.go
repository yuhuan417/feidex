package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	submissionKindReview = "review"
	pendingKindReview    = "review_form"

	reviewTargetUncommitted = "uncommittedChanges"
	reviewTargetBaseBranch  = "baseBranch"
	reviewTargetCommit      = "commit"
	reviewTargetCustom      = "custom"

	reviewFormModeBase   = "base"
	reviewFormModeCommit = "commit"
	reviewFormModeCustom = "custom"

	gitRecordSep = "\x1e"
	gitFieldSep  = "\x1f"
)

type reviewTargetSpec struct {
	Type         string
	Branch       string
	CommitSHA    string
	CommitTitle  string
	Instructions string
}

type reviewPendingPayload struct {
	Mode         string `json:"mode"`
	Branch       string `json:"branch,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	CommitTitle  string `json:"commit_title,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

type reviewBranchOption struct {
	Name      string
	UpdatedAt int64
	Current   bool
	Default   bool
}

type reviewCommitOption struct {
	SHA      string
	ShortSHA string
	Date     string
	Subject  string
}

func isReviewSubmission(sub *state.Submission) bool {
	return sub != nil && strings.TrimSpace(sub.Kind) == submissionKindReview
}

func reviewPendingPayloadFromPending(pending *state.PendingRequest) reviewPendingPayload {
	var payload reviewPendingPayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

func mergeReviewCustomFormValues(payload reviewPendingPayload, values map[string]any) reviewPendingPayload {
	if value, ok := formValueString(values, "instructions"); ok {
		payload.Instructions = value
	}
	return payload
}

func (a *App) commandReview(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) == 0 {
		return a.startInlineReviewFromMessage(msg, reviewTargetSpec{Type: reviewTargetUncommitted})
	}
	switch strings.TrimSpace(args[0]) {
	case "uncommitted", "uncommittedChanges":
		if len(args) != 1 {
			return fmt.Errorf("usage: /review | /review uncommitted | /review base [branch] | /review commit [rev] | /review custom [instructions]")
		}
		return a.startInlineReviewFromMessage(msg, reviewTargetSpec{Type: reviewTargetUncommitted})
	case "base":
		switch len(args) {
		case 1:
			return a.beginReviewForm(msg, reviewFormModeBase)
		case 2:
			return a.startInlineReviewFromMessage(msg, reviewTargetSpec{
				Type:   reviewTargetBaseBranch,
				Branch: strings.TrimSpace(args[1]),
			})
		default:
			return fmt.Errorf("usage: /review base [branch]")
		}
	case "commit":
		switch len(args) {
		case 1:
			return a.beginReviewForm(msg, reviewFormModeCommit)
		case 2:
			return a.startInlineReviewFromMessage(msg, reviewTargetSpec{
				Type:      reviewTargetCommit,
				CommitSHA: strings.TrimSpace(args[1]),
			})
		default:
			return fmt.Errorf("usage: /review commit [rev]")
		}
	case "custom":
		if len(args) == 1 {
			return a.beginReviewForm(msg, reviewFormModeCustom)
		}
		return a.startInlineReviewFromMessage(msg, reviewTargetSpec{
			Type:         reviewTargetCustom,
			Instructions: strings.TrimSpace(strings.Join(args[1:], " ")),
		})
	default:
		return fmt.Errorf("usage: /review | /review uncommitted | /review base [branch] | /review commit [rev] | /review custom [instructions]")
	}
}

func (a *App) renderReviewMenuCard(sessionKey string) map[string]any {
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
	return a.feishu.SimpleStatusCard("代码审查", "blue", menuCardBody("menu.review", strings.Join(bodyLines, "\n")), buttons)
}

func (a *App) beginReviewForm(msg *feishu.InboundMessage, mode string) error {
	sessionKey, _, ws := a.currentWorkspaceForMessage(msg)
	if ws == nil {
		return fmt.Errorf("current workspace not found")
	}
	if strings.TrimSpace(mode) == "" {
		return fmt.Errorf("review form mode is required")
	}
	payload := reviewPendingPayload{Mode: strings.TrimSpace(mode)}
	switch payload.Mode {
	case reviewFormModeBase:
		options, err := a.listReviewBranches(ws.Cwd)
		if err != nil {
			return err
		}
		if len(options) == 0 {
			return fmt.Errorf("当前仓库没有可选 branch")
		}
		payload.Branch = options[0].Name
	case reviewFormModeCommit:
		options, err := a.listReviewCommits(ws.Cwd, 100)
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
	appState := a.appState()
	requestID, err := appState.nextLocalID("review")
	if err != nil {
		return err
	}
	card, err := a.renderReviewFormCard(sessionKey, requestID, payload)
	if err != nil {
		return err
	}
	msgID, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
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

func (a *App) renderReviewFormCard(sessionKey, requestID string, payload reviewPendingPayload) (map[string]any, error) {
	switch strings.TrimSpace(payload.Mode) {
	case reviewFormModeBase:
		return a.renderReviewBaseCard(sessionKey, requestID, payload)
	case reviewFormModeCommit:
		return a.renderReviewCommitCard(sessionKey, requestID, payload)
	case reviewFormModeCustom:
		return a.renderReviewCustomCard(sessionKey, requestID, payload), nil
	default:
		return nil, fmt.Errorf("unsupported review form mode %q", payload.Mode)
	}
}

func (a *App) renderReviewBaseCard(sessionKey, requestID string, payload reviewPendingPayload) (map[string]any, error) {
	ws := a.workspaceForSessionKey(sessionKey)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	options, err := a.listReviewBranches(ws.Cwd)
	if err != nil {
		return nil, err
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("当前仓库没有可选 branch")
	}
	selected := strings.TrimSpace(payload.Branch)
	if selected == "" || !reviewBranchExists(options, selected) {
		selected = options[0].Name
	}
	selectedLabel := selected
	for _, option := range options {
		if option.Name == selected {
			selectedLabel = reviewBranchOptionLabel(option)
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
			Text:  reviewBranchOptionLabel(option),
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

func (a *App) renderReviewCommitCard(sessionKey, requestID string, payload reviewPendingPayload) (map[string]any, error) {
	ws := a.workspaceForSessionKey(sessionKey)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	options, err := a.listReviewCommits(ws.Cwd, 100)
	if err != nil {
		return nil, err
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("当前仓库没有可选 commit")
	}
	selected := strings.TrimSpace(payload.CommitSHA)
	if selected == "" || !reviewCommitExists(options, selected) {
		selected = options[0].SHA
	}
	selectedLabel := selected
	for _, option := range options {
		if option.SHA == selected {
			selectedLabel = reviewCommitOptionLabel(option)
			break
		}
	}
	card := newMarkdownBodyCard("选择 Commit", "blue")
	appendMarkdownBodyCardElement(card, map[string]any{
		"tag": "markdown",
		"content": menuCardBody("menu.review",
			"从最近 100 个 commit 中选择一个 target。\n\n当前选择: `"+inlineCodeText(shortReviewCommitSHA(selected))+"`\n"+selectedLabel),
	})
	selectOptions := make([]selectStaticOption, 0, len(options))
	for _, option := range options {
		selectOptions = append(selectOptions, selectStaticOption{
			Text:  reviewCommitOptionLabel(option),
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

func (a *App) renderReviewCustomCard(sessionKey, requestID string, payload reviewPendingPayload) map[string]any {
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

func (a *App) completeReviewBaseSelect(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, payload, errResp := a.reviewPendingForAction(action, reviewFormModeBase)
	if errResp != nil {
		return errResp, nil
	}
	selected := strings.TrimSpace(action.Option)
	if selected == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效 branch"}}, nil
	}
	payload.Branch = selected
	_ = a.appState().updatePending(pending.ID, func(req *state.PendingRequest) {
		req.PayloadJSON = mustJSON(payload)
	})
	card, err := a.renderReviewBaseCard(pending.SessionKey, pending.ID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Card: rawCard(card)}, nil
}

func (a *App) completeReviewCommitSelect(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, payload, errResp := a.reviewPendingForAction(action, reviewFormModeCommit)
	if errResp != nil {
		return errResp, nil
	}
	selected := strings.TrimSpace(action.Option)
	if selected == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效 commit"}}, nil
	}
	payload.CommitSHA = selected
	if ws := a.workspaceForSessionKey(pending.SessionKey); ws != nil {
		options, err := a.listReviewCommits(ws.Cwd, 100)
		if err == nil {
			for _, option := range options {
				if option.SHA == selected {
					payload.CommitTitle = option.Subject
					break
				}
			}
		}
	}
	_ = a.appState().updatePending(pending.ID, func(req *state.PendingRequest) {
		req.PayloadJSON = mustJSON(payload)
	})
	card, err := a.renderReviewCommitCard(pending.SessionKey, pending.ID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Card: rawCard(card)}, nil
}

func (a *App) completeReviewFormSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	appState := a.appState()
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

	var target reviewTargetSpec
	switch strings.TrimSpace(payload.Mode) {
	case reviewFormModeBase:
		target = reviewTargetSpec{Type: reviewTargetBaseBranch, Branch: strings.TrimSpace(payload.Branch)}
	case reviewFormModeCommit:
		target = reviewTargetSpec{Type: reviewTargetCommit, CommitSHA: strings.TrimSpace(payload.CommitSHA), CommitTitle: strings.TrimSpace(payload.CommitTitle)}
	case reviewFormModeCustom:
		target = reviewTargetSpec{Type: reviewTargetCustom, Instructions: strings.TrimSpace(payload.Instructions)}
	default:
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未知 review 表单"}}, nil
	}
	msg := a.commandMessageFromAction(action, pending.SessionKey, "/review")
	confirmation, err := a.startInlineReview(msg, target)
	if err != nil {
		_ = appState.updatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
		card, renderErr := a.renderReviewFormCard(pending.SessionKey, requestID, payload)
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
		Card:  rawCard(a.feishu.SimpleStatusCard("Review 已启动", "blue", confirmation, nil)),
	}, nil
}

func (a *App) reviewPendingForAction(action *feishu.CardAction, mode string) (*state.PendingRequest, reviewPendingPayload, *callback.CardActionTriggerResponse) {
	requestID := actionStringValue(action, "request_id")
	pending := a.appState().pending(requestID)
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

func (a *App) startInlineReviewFromMessage(msg *feishu.InboundMessage, target reviewTargetSpec) error {
	confirmation, err := a.startInlineReview(msg, target)
	if err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, confirmation, a.replyInThreadEnabled(msg.ChatType))
}

func (a *App) startInlineReview(msg *feishu.InboundMessage, target reviewTargetSpec) (string, error) {
	if msg == nil {
		return "", fmt.Errorf("nil message")
	}
	sessionKey, sess, ws := a.currentWorkspaceForMessage(msg)
	if ws == nil {
		return "", fmt.Errorf("current workspace not found")
	}
	if sessionHasActiveWork(sess) {
		return "", fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	if sess != nil && len(sess.Queue) > 0 {
		return "", fmt.Errorf("当前有排队输入，请先等待处理完成或清理队列")
	}
	if sess != nil && len(sess.StagedImages) > 0 {
		return "", fmt.Errorf("当前有暂存图片输入，请先发送或丢弃")
	}
	resolved, err := a.resolveReviewTarget(ws.Cwd, target)
	if err != nil {
		return "", err
	}
	threadID := ""
	if sess != nil {
		threadID = strings.TrimSpace(sess.ActiveThreadID)
	}
	if err := a.enqueueReviewSubmission(msg, sessionKey, ws, threadID, resolved); err != nil {
		return "", err
	}
	return reviewConfirmationText(resolved), nil
}

func (a *App) enqueueReviewSubmission(msg *feishu.InboundMessage, sessionKey string, ws *config.Workspace, threadID string, target reviewTargetSpec) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("store not initialized")
	}
	if msg == nil {
		return fmt.Errorf("nil message")
	}
	appState := a.appState()
	sess := appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   ws.ID,
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        "idle",
		}
		if err := appState.saveSession(sess); err != nil {
			return err
		}
	}
	hasInFlight := sessionHasInFlightSubmission(sess)
	queueLenBefore := len(sess.Queue)
	shouldAttemptStart := !hasInFlight
	willWaitInQueue := queueLenBefore > 0 || hasInFlight
	if willWaitInQueue {
		sess.Status = "queued"
		if err := appState.saveSession(sess); err != nil {
			return err
		}
	}
	sub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          ws.ID,
		ThreadID:             strings.TrimSpace(threadID),
		UserID:               msg.UserID,
		ChatID:               msg.ChatID,
		TriggerMessageID:     msg.MessageID,
		SourceMessageIDs:     uniqueStrings([]string{msg.MessageID}),
		SourceRootMessageIDs: uniqueStrings([]string{firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))}),
		InputText:            reviewSubmissionInputText(target),
		Kind:                 submissionKindReview,
		ReviewTargetType:     strings.TrimSpace(target.Type),
		ReviewBranch:         strings.TrimSpace(target.Branch),
		ReviewCommitSHA:      strings.TrimSpace(target.CommitSHA),
		ReviewCommitTitle:    strings.TrimSpace(target.CommitTitle),
		ReviewInstructions:   strings.TrimSpace(target.Instructions),
		Status:               "queued",
		WaitedInQueue:        willWaitInQueue,
	}
	id, err := appState.createSubmission(sub)
	if err != nil {
		return err
	}
	sub.ID = id
	if err := appState.queueSubmission(sessionKey, id); err != nil {
		return err
	}
	if shouldAttemptStart {
		if err := a.startNextSubmission(sessionKey); err != nil {
			return err
		}
		if !willWaitInQueue {
			return nil
		}
	}
	a.markSubmissionQueuedReactions(sub)
	a.sendSubmissionQueuedNotice(context.Background(), sub)
	return nil
}

func (a *App) resolveReviewTarget(cwd string, target reviewTargetSpec) (reviewTargetSpec, error) {
	target.Type = strings.TrimSpace(target.Type)
	switch target.Type {
	case reviewTargetUncommitted:
		if _, err := a.gitRepoRoot(cwd); err != nil {
			return reviewTargetSpec{}, err
		}
		hasChanges, err := a.gitHasWorkingTreeChanges(cwd)
		if err != nil {
			return reviewTargetSpec{}, err
		}
		if !hasChanges {
			return reviewTargetSpec{}, fmt.Errorf("当前没有未提交改动")
		}
		return reviewTargetSpec{Type: reviewTargetUncommitted}, nil
	case reviewTargetBaseBranch:
		if _, err := a.gitRepoRoot(cwd); err != nil {
			return reviewTargetSpec{}, err
		}
		target.Branch = strings.TrimSpace(target.Branch)
		if target.Branch == "" {
			return reviewTargetSpec{}, fmt.Errorf("base branch 不能为空")
		}
		if err := a.gitVerifyCommitish(cwd, target.Branch); err != nil {
			return reviewTargetSpec{}, fmt.Errorf("branch 不存在或不可见")
		}
		hasWorkingChanges, err := a.gitHasWorkingTreeChanges(cwd)
		if err != nil {
			return reviewTargetSpec{}, err
		}
		hasCommittedDiff, err := a.gitHasDiffFromBase(cwd, target.Branch)
		if err != nil {
			return reviewTargetSpec{}, err
		}
		if !hasWorkingChanges && !hasCommittedDiff {
			return reviewTargetSpec{}, fmt.Errorf("当前 target 没有可审查差异")
		}
		return reviewTargetSpec{Type: reviewTargetBaseBranch, Branch: target.Branch}, nil
	case reviewTargetCommit:
		if _, err := a.gitRepoRoot(cwd); err != nil {
			return reviewTargetSpec{}, err
		}
		target.CommitSHA = strings.TrimSpace(target.CommitSHA)
		if target.CommitSHA == "" {
			return reviewTargetSpec{}, fmt.Errorf("commit 不能为空")
		}
		resolvedSHA, err := a.gitResolveCommitSHA(cwd, target.CommitSHA)
		if err != nil {
			return reviewTargetSpec{}, fmt.Errorf("commit 不存在或不唯一")
		}
		title := strings.TrimSpace(target.CommitTitle)
		if title == "" {
			title, _ = a.gitCommitTitle(cwd, resolvedSHA)
		}
		return reviewTargetSpec{Type: reviewTargetCommit, CommitSHA: resolvedSHA, CommitTitle: title}, nil
	case reviewTargetCustom:
		target.Instructions = strings.TrimSpace(target.Instructions)
		if target.Instructions == "" {
			return reviewTargetSpec{}, fmt.Errorf("review instructions 不能为空")
		}
		return reviewTargetSpec{Type: reviewTargetCustom, Instructions: target.Instructions}, nil
	default:
		return reviewTargetSpec{}, fmt.Errorf("unsupported review target %q", target.Type)
	}
}

func reviewSubmissionInputText(target reviewTargetSpec) string {
	switch strings.TrimSpace(target.Type) {
	case reviewTargetUncommitted:
		return "Review: uncommitted changes"
	case reviewTargetBaseBranch:
		return "Review: base branch " + strings.TrimSpace(target.Branch)
	case reviewTargetCommit:
		label := shortReviewCommitSHA(target.CommitSHA)
		if strings.TrimSpace(target.CommitTitle) != "" {
			label += " " + strings.TrimSpace(target.CommitTitle)
		}
		return "Review: commit " + strings.TrimSpace(label)
	case reviewTargetCustom:
		return "Review: " + truncate(strings.TrimSpace(target.Instructions), 80)
	default:
		return "Review"
	}
}

func reviewConfirmationText(target reviewTargetSpec) string {
	return "已启动 review，目标：" + reviewTargetSummary(target) + "。"
}

func reviewTargetSummary(target reviewTargetSpec) string {
	switch strings.TrimSpace(target.Type) {
	case reviewTargetUncommitted:
		return "未提交改动"
	case reviewTargetBaseBranch:
		return "base branch `" + inlineCodeText(target.Branch) + "`"
	case reviewTargetCommit:
		if title := strings.TrimSpace(target.CommitTitle); title != "" {
			return "commit `" + inlineCodeText(shortReviewCommitSHA(target.CommitSHA)) + "` " + title
		}
		return "commit `" + inlineCodeText(shortReviewCommitSHA(target.CommitSHA)) + "`"
	case reviewTargetCustom:
		return "自定义 instructions"
	default:
		return "review"
	}
}

func shortReviewCommitSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func reviewBranchOptionLabel(option reviewBranchOption) string {
	prefix := ""
	switch {
	case option.Current && option.Default:
		prefix = "[当前][默认] "
	case option.Current:
		prefix = "[当前] "
	case option.Default:
		prefix = "[默认] "
	}
	return prefix + option.Name
}

func reviewCommitOptionLabel(option reviewCommitOption) string {
	parts := []string{option.ShortSHA}
	if strings.TrimSpace(option.Date) != "" {
		parts = append(parts, option.Date)
	}
	if strings.TrimSpace(option.Subject) != "" {
		parts = append(parts, option.Subject)
	}
	return strings.Join(parts, " | ")
}

func reviewBranchExists(options []reviewBranchOption, name string) bool {
	name = strings.TrimSpace(name)
	for _, option := range options {
		if option.Name == name {
			return true
		}
	}
	return false
}

func reviewCommitExists(options []reviewCommitOption, sha string) bool {
	sha = strings.TrimSpace(sha)
	for _, option := range options {
		if option.SHA == sha {
			return true
		}
	}
	return false
}

func (a *App) workspaceForSessionKey(sessionKey string) *config.Workspace {
	sess := a.appState().session(sessionKey)
	workspaceID := a.defaultWorkspaceID()
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		workspaceID = strings.TrimSpace(sess.WorkspaceID)
	}
	return config.FindWorkspace(a.cfg, workspaceID)
}

func (a *App) startSubmissionReview(ctx context.Context, threadID string, sub *state.Submission) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("nil submission")
	}
	target := reviewTargetFromSubmission(sub)
	if strings.TrimSpace(threadID) == "" {
		return "", fmt.Errorf("review requires an active thread")
	}
	params := map[string]any{
		"threadId": threadID,
		"delivery": "inline",
		"target":   reviewTargetParams(target),
	}
	var reviewResp codexrpc.ReviewStartResult
	if err := a.codex.Call(ctx, "review/start", params, &reviewResp); err != nil {
		return "", err
	}
	turnID := strings.TrimSpace(reviewResp.Turn.ID)
	if turnID == "" {
		return "", fmt.Errorf("review/start returned empty turn id")
	}
	return turnID, nil
}

func reviewTargetFromSubmission(sub *state.Submission) reviewTargetSpec {
	if sub == nil {
		return reviewTargetSpec{}
	}
	return reviewTargetSpec{
		Type:         strings.TrimSpace(sub.ReviewTargetType),
		Branch:       strings.TrimSpace(sub.ReviewBranch),
		CommitSHA:    strings.TrimSpace(sub.ReviewCommitSHA),
		CommitTitle:  strings.TrimSpace(sub.ReviewCommitTitle),
		Instructions: strings.TrimSpace(sub.ReviewInstructions),
	}
}

func reviewTargetParams(target reviewTargetSpec) map[string]any {
	switch strings.TrimSpace(target.Type) {
	case reviewTargetUncommitted:
		return map[string]any{"type": reviewTargetUncommitted}
	case reviewTargetBaseBranch:
		return map[string]any{"type": reviewTargetBaseBranch, "branch": strings.TrimSpace(target.Branch)}
	case reviewTargetCommit:
		params := map[string]any{"type": reviewTargetCommit, "sha": strings.TrimSpace(target.CommitSHA)}
		if title := strings.TrimSpace(target.CommitTitle); title != "" {
			params["title"] = title
		}
		return params
	case reviewTargetCustom:
		return map[string]any{"type": reviewTargetCustom, "instructions": strings.TrimSpace(target.Instructions)}
	default:
		return map[string]any{"type": reviewTargetUncommitted}
	}
}

func (a *App) listReviewBranches(cwd string) ([]reviewBranchOption, error) {
	if _, err := a.gitRepoRoot(cwd); err != nil {
		return nil, err
	}
	output, err := a.gitOutput(cwd, "for-each-ref", "--sort=-committerdate", "--format=%(refname:short)"+gitFieldSep+"%(committerdate:unix)"+gitRecordSep, "refs/heads")
	if err != nil {
		return nil, err
	}
	currentBranch, _ := a.gitCurrentBranch(cwd)
	defaultBranch := ""
	if value, err := a.gitDefaultBranch(cwd); err == nil {
		defaultBranch = value
	}
	options := make([]reviewBranchOption, 0)
	for _, record := range parseGitStructuredOutput(output) {
		fields := strings.Split(record, gitFieldSep)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if name == "" {
			continue
		}
		unixValue, _ := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		options = append(options, reviewBranchOption{
			Name:      name,
			UpdatedAt: unixValue,
			Current:   name == currentBranch,
			Default:   name == defaultBranch,
		})
	}
	if defaultBranch != "" && !reviewBranchExists(options, defaultBranch) {
		options = append(options, reviewBranchOption{Name: defaultBranch, Default: true})
	}
	sort.SliceStable(options, func(i, j int) bool {
		left := options[i]
		right := options[j]
		switch {
		case left.Current != right.Current:
			return left.Current
		case left.Default != right.Default:
			return left.Default
		case left.UpdatedAt != right.UpdatedAt:
			return left.UpdatedAt > right.UpdatedAt
		default:
			return left.Name < right.Name
		}
	})
	return options, nil
}

func (a *App) listReviewCommits(cwd string, limit int) ([]reviewCommitOption, error) {
	if _, err := a.gitRepoRoot(cwd); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	output, err := a.gitOutput(cwd, "log", "--date=short", "--pretty=format:%H"+gitFieldSep+"%h"+gitFieldSep+"%cd"+gitFieldSep+"%s"+gitRecordSep, "-n", strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	options := make([]reviewCommitOption, 0)
	for _, record := range parseGitStructuredOutput(output) {
		fields := strings.Split(record, gitFieldSep)
		if len(fields) < 4 {
			continue
		}
		sha := strings.TrimSpace(fields[0])
		if sha == "" {
			continue
		}
		options = append(options, reviewCommitOption{
			SHA:      sha,
			ShortSHA: strings.TrimSpace(fields[1]),
			Date:     strings.TrimSpace(fields[2]),
			Subject:  strings.TrimSpace(fields[3]),
		})
	}
	return options, nil
}

func parseGitStructuredOutput(output string) []string {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}
	raw := strings.Split(output, gitRecordSep)
	out := make([]string, 0, len(raw))
	for _, record := range raw {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		out = append(out, record)
	}
	return out
}

func (a *App) gitRepoRoot(cwd string) (string, error) {
	output, err := a.gitOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("当前 workspace 不是 git 仓库")
	}
	return strings.TrimSpace(output), nil
}

func (a *App) gitCurrentBranch(cwd string) (string, error) {
	output, err := a.gitOutput(cwd, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (a *App) gitDefaultBranch(cwd string) (string, error) {
	output, err := a.gitOutput(cwd, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(output)
	branch = strings.TrimPrefix(branch, "origin/")
	return strings.TrimSpace(branch), nil
}

func (a *App) gitVerifyCommitish(cwd, ref string) error {
	_, err := a.gitOutput(cwd, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err
}

func (a *App) gitResolveCommitSHA(cwd, ref string) (string, error) {
	output, err := a.gitOutput(cwd, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (a *App) gitCommitTitle(cwd, sha string) (string, error) {
	output, err := a.gitOutput(cwd, "log", "-1", "--format=%s", sha)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (a *App) gitHasWorkingTreeChanges(cwd string) (bool, error) {
	output, err := a.gitOutput(cwd, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

func (a *App) gitHasDiffFromBase(cwd, branch string) (bool, error) {
	return a.gitCommandHasDiff(cwd, "diff", "--quiet", branch+"...HEAD", "--")
}

func (a *App) gitCommandHasDiff(cwd string, args ...string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return false, nil
	}
	var exitErr *exec.ExitError
	if strings.TrimSpace(string(output)) == "" && err == nil {
		return false, nil
	}
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return true, nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		message = err.Error()
	}
	return false, fmt.Errorf("git %s failed: %s", strings.Join(args, " "), message)
}

func (a *App) gitOutput(cwd string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), message)
	}
	return string(output), nil
}
