// Package reviewcmd provides the /review command service extracted from the
// app god package. It handles review command parsing, inline review start,
// submission enqueue, and review form card rendering and interaction.
package reviewcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appcore "feidex/internal/app/appcore"
	apputil "feidex/internal/app/apputil"
	appreview "feidex/internal/app/review"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	SubmissionKindReview = "review"
	PendingKindReview    = "review_form"

	ReviewFormModeBase   = "base"
	ReviewFormModeCommit = "commit"
	ReviewFormModeCustom = "custom"
)

// ---------------------------------------------------------------------------
// Interfaces — what the service needs from the host application
// ---------------------------------------------------------------------------

// AppStateProvider narrows app state access to the session and submission
// operations used by the review service.
type AppStateProvider interface {
	Session(key string) *state.Session
	SaveSession(sess *state.Session) error
	CreateSubmission(sub *state.Submission) (string, error)
	QueueSubmission(sessionKey, submissionID string) error
	Pending(id string) *state.PendingRequest
	SavePending(req *state.PendingRequest) error
	UpdatePending(id string, mutate func(*state.PendingRequest)) error
	NextLocalID(prefix string) (string, error)
}

// WorkspaceProvider narrows workspace access to the lookup operations used
// by the review service.
type WorkspaceProvider interface {
	ReviewWorkspaceForSessionKey(sessionKey string) *config.Workspace
	ReviewDefaultWorkspaceID() string
	ReviewFindWorkspace(workspaceID string) *config.Workspace
}

// ReviewGitProvider narrows git operations to those used by the review
// service for resolving targets and listing options.
type ReviewGitProvider interface {
	ReviewResolveTarget(cwd string, target appreview.TargetSpec) (appreview.TargetSpec, error)
	ReviewListBranches(cwd string) ([]appreview.BranchOption, error)
	ReviewListCommits(cwd string, limit int) ([]appreview.CommitOption, error)
}

// CodexClient is the narrow interface for the Codex RPC client used by the
// review service.
type CodexClient interface {
	Call(ctx context.Context, method string, params any, out any) error
}

// App defines the interface the review service requires from the host
// application. It embeds appcore.AppConfig so that appcore helpers like
// MakeSessionKey, ReplyInThreadEnabled, etc. can be called directly.
type App interface {
	appcore.AppConfig

	// ReviewFeishu returns the Feishu bot client.
	ReviewFeishu() appcore.FeishuClient
	// ReviewAppState returns the narrowed app state provider.
	ReviewAppState() AppStateProvider
	// ReviewWorkspaceProvider returns the narrowed workspace provider.
	ReviewWorkspaceProvider() WorkspaceProvider
	// ReviewGitProvider returns the narrowed git provider.
	ReviewGitProvider() ReviewGitProvider
	// ReviewCodexClient returns the current Codex RPC client.
	ReviewCodexClient() (CodexClient, error)
	// ReviewMakeSessionKey builds a session key from an inbound message.
	ReviewMakeSessionKey(msg *feishu.InboundMessage) string
	// ReviewReplyInThreadEnabled reports whether reply-in-thread is enabled
	// for the given chat type.
	ReviewReplyInThreadEnabled(chatType string) bool
	// ReviewMenuCardBody formats a menu card body with breadcrumb navigation.
	ReviewMenuCardBody(action, body string) string
	// ReviewActionStringValue extracts a string value from a card action.
	ReviewActionStringValue(action *feishu.CardAction, key string) string
	// ReviewCommandMessageFromAction builds an InboundMessage from a card action.
	ReviewCommandMessageFromAction(action *feishu.CardAction, sessionKey, rawCommand string) *feishu.InboundMessage
	// ReviewSessionHasActiveWork reports whether the session has active work.
	ReviewSessionHasActiveWork(sess *state.Session) bool
	// ReviewSessionHasInFlightSubmission reports whether the session has an
	// in-flight submission.
	ReviewSessionHasInFlightSubmission(sess *state.Session) bool
	// ReviewStartNextSubmission starts the next queued submission for the
	// given session.
	ReviewStartNextSubmission(sessionKey string) error
	// ReviewSendSubmissionQueuedNotice sends a queued notice for the submission.
	ReviewSendSubmissionQueuedNotice(ctx context.Context, sub *state.Submission)
	// ReviewMarkSubmissionQueuedReactions marks the submission with queued
	// reactions.
	ReviewMarkSubmissionQueuedReactions(sub *state.Submission)
	// ReviewCompleteAsyncRenderedCardAction runs an action asynchronously and
	// patches the card.
	ReviewCompleteAsyncRenderedCardAction(
		action *feishu.CardAction,
		sessionKey, toastText string,
		preparingCard map[string]any,
		run func() (*callback.CardActionTriggerResponse, error),
		failureCard func(sessionKey, errText string) map[string]any,
		patchWarnMsg string,
	) (*callback.CardActionTriggerResponse, error)
	// ReviewRenderPreparingCard renders a preparing card for review operations.
	ReviewRenderPreparingCard(sessionKey, body string) map[string]any
	// ReviewRenderFailureCard renders a failure card for review operations.
	ReviewRenderFailureCard(sessionKey, errText, retryAction string) map[string]any
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ReviewPendingPayload is the payload stored in a pending review request.
type ReviewPendingPayload struct {
	Mode         string `json:"mode"`
	Branch       string `json:"branch,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	CommitTitle  string `json:"commit_title,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

// ---------------------------------------------------------------------------
// Service — manages /review command actions
// ---------------------------------------------------------------------------

// ReviewFormService manages review command and form actions for a single app
// instance.
type ReviewFormService struct {
	app App
}

// NewReviewFormService creates a new review form service bound to the given
// app.
func NewReviewFormService(app App) ReviewFormService {
	return ReviewFormService{app: app}
}

// ---------------------------------------------------------------------------
// Exported helper functions
// ---------------------------------------------------------------------------

// IsReviewSubmission returns true if the submission is a review submission.
func IsReviewSubmission(sub *state.Submission) bool {
	return sub != nil && strings.TrimSpace(sub.Kind) == SubmissionKindReview
}

// ReviewPendingPayloadFromPending deserializes a ReviewPendingPayload from a
// PendingRequest.
func ReviewPendingPayloadFromPending(pending *state.PendingRequest) ReviewPendingPayload {
	var payload ReviewPendingPayload
	if pending != nil && strings.TrimSpace(pending.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	}
	return payload
}

// ReviewTargetFromSubmission extracts a TargetSpec from a submission.
func ReviewTargetFromSubmission(sub *state.Submission) appreview.TargetSpec {
	if sub == nil {
		return appreview.TargetSpec{}
	}
	return appreview.TargetSpec{
		Type:         strings.TrimSpace(sub.ReviewTargetType),
		Branch:       strings.TrimSpace(sub.ReviewBranch),
		CommitSHA:    strings.TrimSpace(sub.ReviewCommitSHA),
		CommitTitle:  strings.TrimSpace(sub.ReviewCommitTitle),
		Instructions: strings.TrimSpace(sub.ReviewInstructions),
	}
}

// ReviewTargetParams converts a TargetSpec to a map for RPC parameters.
func ReviewTargetParams(target appreview.TargetSpec) map[string]any {
	switch strings.TrimSpace(target.Type) {
	case appreview.TargetUncommitted:
		return map[string]any{"type": appreview.TargetUncommitted}
	case appreview.TargetBaseBranch:
		return map[string]any{"type": appreview.TargetBaseBranch, "branch": strings.TrimSpace(target.Branch)}
	case appreview.TargetCommit:
		params := map[string]any{"type": appreview.TargetCommit, "sha": strings.TrimSpace(target.CommitSHA)}
		if title := strings.TrimSpace(target.CommitTitle); title != "" {
			params["title"] = title
		}
		return params
	case appreview.TargetCustom:
		return map[string]any{"type": appreview.TargetCustom, "instructions": strings.TrimSpace(target.Instructions)}
	default:
		return map[string]any{"type": appreview.TargetUncommitted}
	}
}

// ---------------------------------------------------------------------------
// Command handling
// ---------------------------------------------------------------------------

// CommandReview handles the /review command with optional sub-commands.
func CommandReview(a App, msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) == 0 {
		return startInlineReviewFromMessage(a, msg, appreview.TargetSpec{Type: appreview.TargetUncommitted})
	}
	switch strings.TrimSpace(args[0]) {
	case "uncommitted", "uncommittedChanges":
		if len(args) != 1 {
			return fmt.Errorf("usage: /review | /review uncommitted | /review base [branch] | /review commit [rev] | /review custom [instructions]")
		}
		return startInlineReviewFromMessage(a, msg, appreview.TargetSpec{Type: appreview.TargetUncommitted})
	case "base":
		switch len(args) {
		case 1:
			return NewReviewFormService(a).BeginReviewForm(msg, ReviewFormModeBase)
		case 2:
			return startInlineReviewFromMessage(a, msg, appreview.TargetSpec{
				Type:   appreview.TargetBaseBranch,
				Branch: strings.TrimSpace(args[1]),
			})
		default:
			return fmt.Errorf("usage: /review base [branch]")
		}
	case "commit":
		switch len(args) {
		case 1:
			return NewReviewFormService(a).BeginReviewForm(msg, ReviewFormModeCommit)
		case 2:
			return startInlineReviewFromMessage(a, msg, appreview.TargetSpec{
				Type:      appreview.TargetCommit,
				CommitSHA: strings.TrimSpace(args[1]),
			})
		default:
			return fmt.Errorf("usage: /review commit [rev]")
		}
	case "custom":
		if len(args) == 1 {
			return NewReviewFormService(a).BeginReviewForm(msg, ReviewFormModeCustom)
		}
		return startInlineReviewFromMessage(a, msg, appreview.TargetSpec{
			Type:         appreview.TargetCustom,
			Instructions: strings.TrimSpace(strings.Join(args[1:], " ")),
		})
	default:
		return fmt.Errorf("usage: /review | /review uncommitted | /review base [branch] | /review commit [rev] | /review custom [instructions]")
	}
}

// ---------------------------------------------------------------------------
// Inline review start
// ---------------------------------------------------------------------------

func startInlineReviewFromMessage(a App, msg *feishu.InboundMessage, target appreview.TargetSpec) error {
	confirmation, err := StartInlineReview(a, msg, target)
	if err != nil {
		return err
	}
	return a.ReviewFeishu().ReplyText(context.Background(), msg.MessageID, confirmation, a.ReviewReplyInThreadEnabled(msg.ChatType))
}

// StartInlineReview starts an inline review for the given target.
func StartInlineReview(a App, msg *feishu.InboundMessage, target appreview.TargetSpec) (string, error) {
	if msg == nil {
		return "", fmt.Errorf("nil message")
	}
	sessionKey := a.ReviewMakeSessionKey(msg)
	wp := a.ReviewWorkspaceProvider()
	stateProvider := a.ReviewAppState()
	sess := stateProvider.Session(sessionKey)
	ws := wp.ReviewWorkspaceForSessionKey(sessionKey)
	if ws == nil {
		return "", fmt.Errorf("current workspace not found")
	}
	if a.ReviewSessionHasActiveWork(sess) {
		return "", fmt.Errorf("当前任务仍在运行，请先等待结束或中断")
	}
	if sess != nil && len(sess.Queue) > 0 {
		return "", fmt.Errorf("当前有排队输入，请先等待处理完成或清理队列")
	}
	if sess != nil && len(sess.StagedImages) > 0 {
		return "", fmt.Errorf("当前有暂存图片输入，请先发送或丢弃")
	}
	resolved, err := a.ReviewGitProvider().ReviewResolveTarget(ws.Cwd, target)
	if err != nil {
		return "", err
	}
	threadID := ""
	if sess != nil {
		threadID = strings.TrimSpace(sess.ActiveThreadID)
	}
	if err := EnqueueReviewSubmission(a, msg, sessionKey, ws, threadID, resolved); err != nil {
		return "", err
	}
	return appreview.ConfirmationText(resolved), nil
}

// ---------------------------------------------------------------------------
// Submission enqueue
// ---------------------------------------------------------------------------

// EnqueueReviewSubmission enqueues a review submission for processing.
func EnqueueReviewSubmission(a App, msg *feishu.InboundMessage, sessionKey string, ws *config.Workspace, threadID string, target appreview.TargetSpec) error {
	if msg == nil {
		return fmt.Errorf("nil message")
	}
	stateProvider := a.ReviewAppState()
	store := a.Store()
	if store == nil {
		return fmt.Errorf("store not initialized")
	}
	sess := stateProvider.Session(sessionKey)
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
		if err := stateProvider.SaveSession(sess); err != nil {
			return err
		}
	}
	hasInFlight := a.ReviewSessionHasInFlightSubmission(sess)
	queueLenBefore := len(sess.Queue)
	shouldAttemptStart := !hasInFlight
	willWaitInQueue := queueLenBefore > 0 || hasInFlight
	if willWaitInQueue {
		sess.Status = "queued"
		if err := stateProvider.SaveSession(sess); err != nil {
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
		SourceRootMessageIDs: uniqueStrings([]string{apputil.FirstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))}),
		InputText:            appreview.SubmissionInputText(target),
		Kind:                 SubmissionKindReview,
		ReviewTargetType:     strings.TrimSpace(target.Type),
		ReviewBranch:         strings.TrimSpace(target.Branch),
		ReviewCommitSHA:      strings.TrimSpace(target.CommitSHA),
		ReviewCommitTitle:    strings.TrimSpace(target.CommitTitle),
		ReviewInstructions:   strings.TrimSpace(target.Instructions),
		Status:               "queued",
		WaitedInQueue:        willWaitInQueue,
	}
	id, err := stateProvider.CreateSubmission(sub)
	if err != nil {
		return err
	}
	sub.ID = id
	if err := stateProvider.QueueSubmission(sessionKey, id); err != nil {
		return err
	}
	if shouldAttemptStart {
		if err := a.ReviewStartNextSubmission(sessionKey); err != nil {
			return err
		}
		if !willWaitInQueue {
			return nil
		}
	}
	a.ReviewMarkSubmissionQueuedReactions(sub)
	a.ReviewSendSubmissionQueuedNotice(context.Background(), sub)
	return nil
}

// ---------------------------------------------------------------------------
// Submission review start
// ---------------------------------------------------------------------------

// StartSubmissionReview starts a review for a queued submission.
func StartSubmissionReview(a App, ctx context.Context, threadID string, sub *state.Submission) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("nil submission")
	}
	target := ReviewTargetFromSubmission(sub)
	if strings.TrimSpace(threadID) == "" {
		return "", fmt.Errorf("review requires an active thread")
	}
	params := map[string]any{
		"threadId": threadID,
		"delivery": "inline",
		"target":   ReviewTargetParams(target),
	}
	client, err := a.ReviewCodexClient()
	if err != nil {
		return "", err
	}
	var reviewResp codexrpc.ReviewStartResult
	if err := client.Call(ctx, "review/start", params, &reviewResp); err != nil {
		return "", err
	}
	turnID := strings.TrimSpace(reviewResp.Turn.ID)
	if turnID == "" {
		return "", fmt.Errorf("review/start returned empty turn id")
	}
	return turnID, nil
}

// ---------------------------------------------------------------------------
// Review form methods
// ---------------------------------------------------------------------------

// RenderReviewMenuCard renders the review menu card.
func (s ReviewFormService) RenderReviewMenuCard(sessionKey string) map[string]any {
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
	return s.app.ReviewFeishu().SimpleStatusCard("代码审查", "blue", s.app.ReviewMenuCardBody("menu.review", strings.Join(bodyLines, "\n")), buttons)
}

// BeginReviewForm starts a review form interaction for the given mode.
func (s ReviewFormService) BeginReviewForm(msg *feishu.InboundMessage, mode string) error {
	sessionKey := s.app.ReviewMakeSessionKey(msg)
	wp := s.app.ReviewWorkspaceProvider()
	ws := wp.ReviewWorkspaceForSessionKey(sessionKey)
	if ws == nil {
		return fmt.Errorf("current workspace not found")
	}
	if strings.TrimSpace(mode) == "" {
		return fmt.Errorf("review form mode is required")
	}
	payload := ReviewPendingPayload{Mode: strings.TrimSpace(mode)}
	switch payload.Mode {
	case ReviewFormModeBase:
		options, err := s.app.ReviewGitProvider().ReviewListBranches(ws.Cwd)
		if err != nil {
			return err
		}
		if len(options) == 0 {
			return fmt.Errorf("当前仓库没有可选 branch")
		}
		payload.Branch = options[0].Name
	case ReviewFormModeCommit:
		options, err := s.app.ReviewGitProvider().ReviewListCommits(ws.Cwd, 100)
		if err != nil {
			return err
		}
		if len(options) == 0 {
			return fmt.Errorf("当前仓库没有可选 commit")
		}
		payload.CommitSHA = options[0].SHA
		payload.CommitTitle = options[0].Subject
	case ReviewFormModeCustom:
	default:
		return fmt.Errorf("unsupported review form mode %q", payload.Mode)
	}
	stateProvider := s.app.ReviewAppState()
	requestID, err := stateProvider.NextLocalID("review")
	if err != nil {
		return err
	}
	card, err := s.RenderReviewFormCard(sessionKey, requestID, payload)
	if err != nil {
		return err
	}
	msgID, err := s.app.ReviewFeishu().ReplyCard(context.Background(), msg.MessageID, card, s.app.ReviewReplyInThreadEnabled(msg.ChatType))
	if err != nil {
		return err
	}
	return stateProvider.SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        PendingKindReview,
		SessionKey:  sessionKey,
		OwnerUserID: msg.UserID,
		FeishuMsgID: msgID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	})
}

// RenderReviewFormCard dispatches to the appropriate form card renderer based
// on the payload mode.
func (s ReviewFormService) RenderReviewFormCard(sessionKey, requestID string, payload ReviewPendingPayload) (map[string]any, error) {
	switch strings.TrimSpace(payload.Mode) {
	case ReviewFormModeBase:
		return s.RenderReviewBaseCard(sessionKey, requestID, payload)
	case ReviewFormModeCommit:
		return s.RenderReviewCommitCard(sessionKey, requestID, payload)
	case ReviewFormModeCustom:
		return s.RenderReviewCustomCard(sessionKey, requestID, payload), nil
	default:
		return nil, fmt.Errorf("unsupported review form mode %q", payload.Mode)
	}
}

// RenderReviewBaseCard renders the base branch selection card.
func (s ReviewFormService) RenderReviewBaseCard(sessionKey, requestID string, payload ReviewPendingPayload) (map[string]any, error) {
	wp := s.app.ReviewWorkspaceProvider()
	ws := wp.ReviewWorkspaceForSessionKey(sessionKey)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	options, err := s.app.ReviewGitProvider().ReviewListBranches(ws.Cwd)
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
	bodyText := s.app.ReviewMenuCardBody("menu.review",
		"选择一个 base branch，然后开始 review。\n\n当前选择: `"+apputil.InlineCodeText(selected)+"`\n"+selectedLabel)
	return appreview.RenderBaseBranchFormCard(sessionKey, requestID, bodyText, selectedLabel, options, selected), nil
}

// RenderReviewCommitCard renders the commit selection card.
func (s ReviewFormService) RenderReviewCommitCard(sessionKey, requestID string, payload ReviewPendingPayload) (map[string]any, error) {
	wp := s.app.ReviewWorkspaceProvider()
	ws := wp.ReviewWorkspaceForSessionKey(sessionKey)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	options, err := s.app.ReviewGitProvider().ReviewListCommits(ws.Cwd, 100)
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
	bodyText := s.app.ReviewMenuCardBody("menu.review",
		"从最近 100 个 commit 中选择一个 target。\n\n当前选择: `"+apputil.InlineCodeText(appreview.ShortCommitSHA(selected))+"`\n"+selectedLabel)
	return appreview.RenderCommitFormCard(sessionKey, requestID, bodyText, selectedLabel, options, selected), nil
}

// RenderReviewCustomCard renders the custom review instructions card.
func (s ReviewFormService) RenderReviewCustomCard(sessionKey, requestID string, payload ReviewPendingPayload) map[string]any {
	bodyText := s.app.ReviewMenuCardBody("menu.review", "填写 review instructions，然后开始 review。")
	return appreview.RenderCustomFormCard(sessionKey, requestID, bodyText, payload.Instructions)
}

// CompleteReviewBaseSelect handles a base branch selection action.
func (s ReviewFormService) CompleteReviewBaseSelect(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, _, errResp := s.reviewPendingForAction(action, ReviewFormModeBase)
	if errResp != nil {
		return errResp, nil
	}
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return s.completeReviewBaseSelectSync(action)
	}
	return s.app.ReviewCompleteAsyncRenderedCardAction(
		action,
		pending.SessionKey,
		"正在刷新 review 选项",
		s.app.ReviewRenderPreparingCard(pending.SessionKey, "正在刷新 base branch 选择，请稍候。\n\n这张卡片会自动刷新。"),
		func() (*callback.CardActionTriggerResponse, error) {
			return s.completeReviewBaseSelectSync(action)
		},
		func(sessionKey, errText string) map[string]any {
			return s.app.ReviewRenderFailureCard(sessionKey, errText, "")
		},
		"review base select patch failed",
	)
}

func (s ReviewFormService) completeReviewBaseSelectSync(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, payload, errResp := s.reviewPendingForAction(action, ReviewFormModeBase)
	if errResp != nil {
		return errResp, nil
	}
	selected := strings.TrimSpace(action.Option)
	if selected == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效 branch"}}, nil
	}
	payload.Branch = selected
	_ = s.app.ReviewAppState().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.PayloadJSON = mustJSON(payload)
	})
	card, err := s.RenderReviewBaseCard(pending.SessionKey, pending.ID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Card: rawCard(card)}, nil
}

// CompleteReviewCommitSelect handles a commit selection action.
func (s ReviewFormService) CompleteReviewCommitSelect(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, _, errResp := s.reviewPendingForAction(action, ReviewFormModeCommit)
	if errResp != nil {
		return errResp, nil
	}
	if action == nil || strings.TrimSpace(action.MessageID) == "" {
		return s.completeReviewCommitSelectSync(action)
	}
	return s.app.ReviewCompleteAsyncRenderedCardAction(
		action,
		pending.SessionKey,
		"正在刷新 review 选项",
		s.app.ReviewRenderPreparingCard(pending.SessionKey, "正在刷新 commit 选择，请稍候。\n\n这张卡片会自动刷新。"),
		func() (*callback.CardActionTriggerResponse, error) {
			return s.completeReviewCommitSelectSync(action)
		},
		func(sessionKey, errText string) map[string]any {
			return s.app.ReviewRenderFailureCard(sessionKey, errText, "")
		},
		"review commit select patch failed",
	)
}

func (s ReviewFormService) completeReviewCommitSelectSync(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, payload, errResp := s.reviewPendingForAction(action, ReviewFormModeCommit)
	if errResp != nil {
		return errResp, nil
	}
	selected := strings.TrimSpace(action.Option)
	if selected == "" {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未收到有效 commit"}}, nil
	}
	payload.CommitSHA = selected
	wp := s.app.ReviewWorkspaceProvider()
	if ws := wp.ReviewWorkspaceForSessionKey(pending.SessionKey); ws != nil {
		options, err := s.app.ReviewGitProvider().ReviewListCommits(ws.Cwd, 100)
		if err == nil {
			for _, option := range options {
				if option.SHA == selected {
					payload.CommitTitle = option.Subject
					break
				}
			}
		}
	}
	_ = s.app.ReviewAppState().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.PayloadJSON = mustJSON(payload)
	})
	card, err := s.RenderReviewCommitCard(pending.SessionKey, pending.ID, payload)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Card: rawCard(card)}, nil
}

// CompleteReviewFormSubmit handles a review form submission action.
func (s ReviewFormService) CompleteReviewFormSubmit(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	stateProvider := s.app.ReviewAppState()
	requestID := s.app.ReviewActionStringValue(action, "request_id")
	pending := stateProvider.Pending(requestID)
	if pending == nil || pending.Kind != PendingKindReview {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "review 请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个 review 请求"}}, nil
	}
	payload := ReviewPendingPayloadFromPending(pending)
	if action == nil || strings.TrimSpace(action.MessageID) == "" || strings.TrimSpace(payload.Mode) == ReviewFormModeCustom {
		return s.completeReviewFormSubmitSync(action)
	}
	return s.app.ReviewCompleteAsyncRenderedCardAction(
		action,
		pending.SessionKey,
		"正在启动 review",
		s.app.ReviewRenderPreparingCard(pending.SessionKey, "正在启动 review，请稍候。\n\n这张卡片会自动刷新。"),
		func() (*callback.CardActionTriggerResponse, error) {
			return s.completeReviewFormSubmitSync(action)
		},
		func(sessionKey, errText string) map[string]any {
			return s.app.ReviewRenderFailureCard(sessionKey, errText, "")
		},
		"review submit patch failed",
	)
}

func (s ReviewFormService) completeReviewFormSubmitSync(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	stateProvider := s.app.ReviewAppState()
	requestID := s.app.ReviewActionStringValue(action, "request_id")
	pending := stateProvider.Pending(requestID)
	if pending == nil || pending.Kind != PendingKindReview {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "review 请求已过期"}}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个 review 请求"}}, nil
	}
	payload := ReviewPendingPayloadFromPending(pending)
	payload = mergeReviewCustomFormValues(payload, action.FormValue)

	var target appreview.TargetSpec
	switch strings.TrimSpace(payload.Mode) {
	case ReviewFormModeBase:
		target = appreview.TargetSpec{Type: appreview.TargetBaseBranch, Branch: strings.TrimSpace(payload.Branch)}
	case ReviewFormModeCommit:
		target = appreview.TargetSpec{Type: appreview.TargetCommit, CommitSHA: strings.TrimSpace(payload.CommitSHA), CommitTitle: strings.TrimSpace(payload.CommitTitle)}
	case ReviewFormModeCustom:
		target = appreview.TargetSpec{Type: appreview.TargetCustom, Instructions: strings.TrimSpace(payload.Instructions)}
	default:
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "未知 review 表单"}}, nil
	}
	msg := s.app.ReviewCommandMessageFromAction(action, pending.SessionKey, "/review")
	confirmation, err := StartInlineReview(s.app, msg, target)
	if err != nil {
		_ = stateProvider.UpdatePending(requestID, func(req *state.PendingRequest) { req.PayloadJSON = mustJSON(payload) })
		card, renderErr := s.RenderReviewFormCard(pending.SessionKey, requestID, payload)
		if renderErr != nil {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: err.Error()},
			Card:  rawCard(card),
		}, nil
	}
	_ = stateProvider.UpdatePending(requestID, func(req *state.PendingRequest) {
		req.Status = "resolved"
		req.PayloadJSON = mustJSON(payload)
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已启动 review"},
		Card:  rawCard(s.app.ReviewFeishu().SimpleStatusCard("Review 已启动", "blue", confirmation, nil)),
	}, nil
}

func (s ReviewFormService) reviewPendingForAction(action *feishu.CardAction, mode string) (*state.PendingRequest, ReviewPendingPayload, *callback.CardActionTriggerResponse) {
	requestID := s.app.ReviewActionStringValue(action, "request_id")
	pending := s.app.ReviewAppState().Pending(requestID)
	if pending == nil || pending.Kind != PendingKindReview {
		return nil, ReviewPendingPayload{}, &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "review 请求已过期"}}
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return nil, ReviewPendingPayload{}, &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个 review 请求"}}
	}
	payload := ReviewPendingPayloadFromPending(pending)
	if strings.TrimSpace(payload.Mode) != strings.TrimSpace(mode) {
		return nil, ReviewPendingPayload{}, &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "review 请求类型不匹配"}}
	}
	return pending, payload, nil
}

// ---------------------------------------------------------------------------
// Local helpers (not exported)
// ---------------------------------------------------------------------------

func mergeReviewCustomFormValues(payload ReviewPendingPayload, values map[string]any) ReviewPendingPayload {
	if value, ok := apputil.FormValueString(values, "instructions"); ok {
		payload.Instructions = value
	}
	return payload
}

func uniqueStrings(vals []string) []string {
	seen := make(map[string]struct{}, len(vals))
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func commandLabel(label, slash string) string {
	label = strings.TrimSpace(label)
	slash = strings.TrimSpace(slash)
	if label == "" {
		return slash
	}
	if slash == "" {
		return label
	}
	return label + " " + slash
}

func submenuCommandLabel(label, slash string) string {
	l := commandLabel(label, slash)
	l = strings.TrimSpace(l)
	if l == "" {
		return "›"
	}
	return l + " ›"
}

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
