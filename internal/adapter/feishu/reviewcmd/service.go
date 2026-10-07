// Package reviewcmd provides the /review command service extracted from the
// app god package. It handles review command parsing, inline review start,
// submission enqueue, and review form card rendering and interaction.
package reviewcmd

import (
	"context"
	"encoding/json"
	menuutil "feidex/internal/adapter/feishu/menuutil"
	interactionapp "feidex/internal/application/interaction"
	reviewapp "feidex/internal/application/review"
	"feidex/internal/application/workspace"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"fmt"
	"strings"
	"sync"

	apputil "feidex/internal/formatutil"

	appreview "feidex/internal/adapter/feishu/review"
	"feidex/internal/application/backendops"
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

// StateProvider narrows app state access to the session and submission
// operations used by the review service.
type StateProvider interface {
	Session(key string) *conversation.Session
	QueueSubmission(sessionKey, submissionID string) error
	Pending(id string) *state.PendingRequest
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
	StartReview(context.Context, backendops.ReviewRequest) (backendops.ReviewResult, error)
}

// Outbound is the semantic messaging capability used by review commands.
type Outbound interface {
	ReplyInteractionCard(context.Context, string, string, map[string]any, bool) (string, error)
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	ReplyText(context.Context, string, string, bool) error
}

type CardRenderer interface {
	SimpleStatusCard(string, string, string, []feishu.Button) map[string]any
}

// Dependencies is the explicit review capability set assembled by the
// composition root.
type Dependencies struct {
	UseCase        *reviewapp.Service
	ConfigProvider interface {
		Config() *config.Config
		ConfigMu() *sync.RWMutex
		Backend() string
		FrontendID() string
		FrontendConfigIndex() int
		Store() *state.Store
		WorkspaceSelection() workspace.SelectionService
	}
	Outbound                          Outbound
	CardRenderer                      CardRenderer
	StateProvider                     StateProvider
	WorkspaceProviderValue            WorkspaceProvider
	GitProvider                       ReviewGitProvider
	CodexClientFn                     func() (CodexClient, error)
	MakeSessionKeyFn                  func(*feishu.InboundMessage) string
	ReplyInThreadEnabledFn            func(string) bool
	MenuCardBodyFn                    func(string, string) string
	ActionStringValueFn               func(*feishu.CardAction, string) string
	CommandMessageFromActionFn        func(*feishu.CardAction, string, string) *feishu.InboundMessage
	SessionHasActiveWorkFn            func(*conversation.Session) bool
	SessionHasInFlightSubmissionFn    func(*conversation.Session) bool
	StartNextSubmissionFn             func(string) error
	SendSubmissionQueuedNoticeFn      func(context.Context, *domainsubmission.Submission)
	MarkSubmissionQueuedReactionsFn   func(*domainsubmission.Submission)
	CompleteAsyncCommandActionFn      func(*feishu.CardAction, string, string, string, string, map[string]any, func(string, string) map[string]any, func(string, string) map[string]any, string) (*callback.CardActionTriggerResponse, error)
	CompleteAsyncRenderedCardActionFn func(*feishu.CardAction, string, string, map[string]any, func() (*callback.CardActionTriggerResponse, error), func(string, string) map[string]any, string) (*callback.CardActionTriggerResponse, error)
	ContextProvider                   interface{ Context() context.Context }
}

func (d Dependencies) Context() context.Context {
	if d.ContextProvider != nil {
		if ctx := d.ContextProvider.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}

func (d Dependencies) Config() *config.Config {
	if d.ConfigProvider == nil {
		return nil
	}
	return d.ConfigProvider.Config()
}
func (d Dependencies) ConfigMu() *sync.RWMutex {
	if d.ConfigProvider == nil {
		return nil
	}
	return d.ConfigProvider.ConfigMu()
}
func (d Dependencies) Backend() string {
	if d.ConfigProvider == nil {
		return ""
	}
	return d.ConfigProvider.Backend()
}
func (d Dependencies) FrontendID() string {
	if d.ConfigProvider == nil {
		return ""
	}
	return d.ConfigProvider.FrontendID()
}
func (d Dependencies) FrontendConfigIndex() int {
	if d.ConfigProvider == nil {
		return -1
	}
	return d.ConfigProvider.FrontendConfigIndex()
}
func (d Dependencies) Store() *state.Store {
	if d.ConfigProvider == nil {
		return nil
	}
	return d.ConfigProvider.Store()
}
func (d Dependencies) ReviewOutbound() Outbound                   { return d.Outbound }
func (d Dependencies) ReviewRenderer() CardRenderer               { return d.CardRenderer }
func (d Dependencies) ReviewAppState() StateProvider              { return d.StateProvider }
func (d Dependencies) ReviewWorkspaceProvider() WorkspaceProvider { return d.WorkspaceProviderValue }
func (d Dependencies) ReviewGitProvider() ReviewGitProvider       { return d.GitProvider }
func (d Dependencies) ReviewCodexClient() (CodexClient, error) {
	if d.CodexClientFn == nil {
		return nil, fmt.Errorf("codex client unavailable")
	}
	return d.CodexClientFn()
}
func (d Dependencies) ReviewMakeSessionKey(m *feishu.InboundMessage) string {
	if d.MakeSessionKeyFn == nil {
		return ""
	}
	return d.MakeSessionKeyFn(m)
}
func (d Dependencies) ReviewReplyInThreadEnabled(v string) bool {
	return d.ReplyInThreadEnabledFn != nil && d.ReplyInThreadEnabledFn(v)
}
func (d Dependencies) ReviewMenuCardBody(a, b string) string {
	if d.MenuCardBodyFn == nil {
		return b
	}
	return d.MenuCardBodyFn(a, b)
}
func (d Dependencies) ReviewActionStringValue(a *feishu.CardAction, k string) string {
	if d.ActionStringValueFn == nil {
		return ""
	}
	return d.ActionStringValueFn(a, k)
}
func (d Dependencies) ReviewCommandMessageFromAction(a *feishu.CardAction, s, r string) *feishu.InboundMessage {
	if d.CommandMessageFromActionFn == nil {
		return nil
	}
	return d.CommandMessageFromActionFn(a, s, r)
}
func (d Dependencies) ReviewSessionHasActiveWork(s *conversation.Session) bool {
	return d.SessionHasActiveWorkFn != nil && d.SessionHasActiveWorkFn(s)
}
func (d Dependencies) ReviewSessionHasInFlightSubmission(s *conversation.Session) bool {
	return d.SessionHasInFlightSubmissionFn != nil && d.SessionHasInFlightSubmissionFn(s)
}
func (d Dependencies) ReviewStartNextSubmission(s string) error {
	if d.StartNextSubmissionFn == nil {
		return fmt.Errorf("submission starter unavailable")
	}
	return d.StartNextSubmissionFn(s)
}
func (d Dependencies) ReviewSendSubmissionQueuedNotice(c context.Context, s *domainsubmission.Submission) {
	if d.SendSubmissionQueuedNoticeFn != nil {
		d.SendSubmissionQueuedNoticeFn(c, s)
	}
}
func (d Dependencies) ReviewMarkSubmissionQueuedReactions(s *domainsubmission.Submission) {
	if d.MarkSubmissionQueuedReactionsFn != nil {
		d.MarkSubmissionQueuedReactionsFn(s)
	}
}
func (d Dependencies) ReviewCompleteAsyncCommandAction(a *feishu.CardAction, s, r, f, t string, p map[string]any, ok, fail func(string, string) map[string]any, w string) (*callback.CardActionTriggerResponse, error) {
	if d.CompleteAsyncCommandActionFn == nil {
		return nil, fmt.Errorf("async command unavailable")
	}
	return d.CompleteAsyncCommandActionFn(a, s, r, f, t, p, ok, fail, w)
}
func (d Dependencies) ReviewCompleteAsyncRenderedCardAction(a *feishu.CardAction, s, t string, p map[string]any, r func() (*callback.CardActionTriggerResponse, error), f func(string, string) map[string]any, w string) (*callback.CardActionTriggerResponse, error) {
	if d.CompleteAsyncRenderedCardActionFn == nil {
		return nil, fmt.Errorf("async card action unavailable")
	}
	return d.CompleteAsyncRenderedCardActionFn(a, s, t, p, r, f, w)
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ReviewPendingPayload is the payload stored in a pending review request.
type ReviewPendingPayload = reviewapp.FormPayload

// ---------------------------------------------------------------------------
// Service — manages /review command actions
// ---------------------------------------------------------------------------

// ReviewFormService manages review command and form actions for a single app
// instance.
type ReviewFormService struct {
	app Dependencies
}

// NewReviewFormService creates a new review form service bound to the given
// app.
func NewReviewFormService(app Dependencies) ReviewFormService {
	return ReviewFormService{app: app}
}

func (s ReviewFormService) CommandReview(msg *feishu.InboundMessage, args []string) error {
	return CommandReview(s.app, msg, args)
}

// ---------------------------------------------------------------------------
// Exported helper functions
// ---------------------------------------------------------------------------

// IsReviewSubmission returns true if the submission is a review submission.
func IsReviewSubmission(sub *domainsubmission.Submission) bool {
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
func ReviewTargetFromSubmission(sub *domainsubmission.Submission) appreview.TargetSpec {
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

// ---------------------------------------------------------------------------
// Command handling
// ---------------------------------------------------------------------------

// CommandReview handles the /review command with optional sub-commands.
func CommandReview(a Dependencies, msg *feishu.InboundMessage, args []string) error {
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

func startInlineReviewFromMessage(a Dependencies, msg *feishu.InboundMessage, target appreview.TargetSpec) error {
	confirmation, err := StartInlineReview(a, msg, target)
	if err != nil {
		return err
	}
	return a.ReviewOutbound().ReplyText(a.Context(), msg.MessageID, confirmation, a.ReviewReplyInThreadEnabled(msg.ChatType))
}

// StartInlineReview starts an inline review for the given target.
func StartInlineReview(a Dependencies, msg *feishu.InboundMessage, target appreview.TargetSpec) (string, error) {
	if msg == nil {
		return "", fmt.Errorf("nil message")
	}
	key := a.ReviewMakeSessionKey(msg)
	ws := a.ReviewWorkspaceProvider().ReviewWorkspaceForSessionKey(key)
	if ws == nil {
		return "", fmt.Errorf("current workspace not found")
	}
	resolved, err := a.UseCase.Start(a.Context(), reviewapp.Input{SessionKey: key, UserID: msg.UserID, ChatID: msg.ChatID, ChatType: msg.ChatType, MessageID: msg.MessageID, RootMessageID: msg.RootMessageID}, reviewapp.Workspace{ID: ws.ID, CWD: ws.Cwd}, target)
	if err != nil {
		return "", err
	}
	return appreview.ConfirmationText(resolved), nil
}

// ---------------------------------------------------------------------------
// Submission review start
// ---------------------------------------------------------------------------

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
			Value: map[string]any{"action": "review.start.uncommitted", "session_key": sessionKey},
		},
		{
			Text:  submenuCommandLabel("对比分支审查", "/review base"),
			Type:  "default",
			Value: map[string]any{"action": "review.start.base", "session_key": sessionKey},
		},
		{
			Text:  submenuCommandLabel("审查单个 commit", "/review commit"),
			Type:  "default",
			Value: map[string]any{"action": "review.start.commit", "session_key": sessionKey},
		},
		{
			Text:  submenuCommandLabel("自定义审查", "/review custom"),
			Type:  "default",
			Value: map[string]any{"action": "review.start.custom", "session_key": sessionKey},
		},
	}
	return menuutil.PageCard{
		Node: "menu.review", SessionKey: sessionKey, Title: "代码审查", Color: "blue",
		Body:    strings.Join(bodyLines, "\n"),
		Buttons: buttons,
	}.Render()
}

// BeginReviewForm starts a review form interaction for the given mode.
func (s ReviewFormService) BeginReviewForm(msg *feishu.InboundMessage, mode string) error {
	key := s.app.ReviewMakeSessionKey(msg)
	ws := s.app.ReviewWorkspaceProvider().ReviewWorkspaceForSessionKey(key)
	if ws == nil {
		return fmt.Errorf("current workspace not found")
	}
	req, _, err := s.app.UseCase.OpenForm(reviewapp.Input{SessionKey: key, UserID: msg.UserID}, reviewapp.Workspace{ID: ws.ID, CWD: ws.Cwd}, mode)
	if err != nil {
		return err
	}
	return s.app.UseCase.Delivery.Open(s.app.Context(), interactionapp.DeliveryInput{Request: *req, NonBlocking: true}, formPresenter{service: s, message: msg})
}

type formPresenter struct {
	service ReviewFormService
	message *feishu.InboundMessage
}

func (p formPresenter) DeliverInteraction(ctx context.Context, input interactionapp.DeliveryInput) (string, error) {
	payload := ReviewPendingPayloadFromPending(&input.Request)
	card, err := p.service.RenderReviewFormCard(input.Request.SessionKey, input.Request.ID, payload)
	if err != nil {
		return "", err
	}
	return p.service.app.ReviewOutbound().ReplyInteractionCard(ctx, input.Request.ID, p.message.MessageID, card, p.service.app.ReviewReplyInThreadEnabled(p.message.ChatType))
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
func (s ReviewFormService) selectForm(action *feishu.CardAction, mode string) (*callback.CardActionTriggerResponse, error) {
	pending, _, resp := s.reviewPendingForAction(action, mode)
	if resp != nil {
		return resp, nil
	}
	ws := s.app.ReviewWorkspaceProvider().ReviewWorkspaceForSessionKey(pending.SessionKey)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	_, payload, err := s.app.UseCase.Select(pending.ID, action.UserID, mode, action.Option, reviewapp.Workspace{ID: ws.ID, CWD: ws.Cwd})
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	card, err := s.RenderReviewFormCard(pending.SessionKey, pending.ID, payload)
	if err != nil {
		return nil, err
	}
	return &callback.CardActionTriggerResponse{Card: rawCard(card)}, nil
}
func (s ReviewFormService) CompleteReviewBaseSelect(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	pending, _, resp := s.reviewPendingForAction(action, ReviewFormModeBase)
	if resp != nil {
		return resp, nil
	}
	if strings.TrimSpace(action.MessageID) == "" {
		return s.selectForm(action, ReviewFormModeBase)
	}
	return s.app.ReviewCompleteAsyncRenderedCardAction(action, pending.SessionKey, "正在刷新 review 选项", renderReviewPreparingCard(s.app, pending.SessionKey, "正在刷新 base branch 选择，请稍候。\n\n这张卡片会自动刷新。"), func() (*callback.CardActionTriggerResponse, error) { return s.selectForm(action, ReviewFormModeBase) }, func(sessionKey, errText string) map[string]any {
		return renderReviewFailureCard(s.app, sessionKey, errText, "")
	}, "review base select patch failed")
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
		renderReviewPreparingCard(s.app, pending.SessionKey, "正在刷新 commit 选择，请稍候。\n\n这张卡片会自动刷新。"),
		func() (*callback.CardActionTriggerResponse, error) {
			return s.completeReviewCommitSelectSync(action)
		},
		func(sessionKey, errText string) map[string]any {
			return renderReviewFailureCard(s.app, sessionKey, errText, "")
		},
		"review commit select patch failed",
	)
}

func (s ReviewFormService) completeReviewCommitSelectSync(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.selectForm(action, ReviewFormModeCommit)
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
		renderReviewPreparingCard(s.app, pending.SessionKey, "正在启动 review，请稍候。\n\n这张卡片会自动刷新。"),
		func() (*callback.CardActionTriggerResponse, error) {
			return s.completeReviewFormSubmitSync(action)
		},
		func(sessionKey, errText string) map[string]any {
			return renderReviewFailureCard(s.app, sessionKey, errText, "")
		},
		"review submit patch failed",
	)
}

func (s ReviewFormService) completeReviewFormSubmitSync(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	id := s.app.ReviewActionStringValue(action, "request_id")
	pending, _, err := s.app.UseCase.Form(id, action.UserID, "")
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	ws := s.app.ReviewWorkspaceProvider().ReviewWorkspaceForSessionKey(pending.SessionKey)
	if ws == nil {
		return nil, fmt.Errorf("current workspace not found")
	}
	msg := s.app.ReviewCommandMessageFromAction(action, pending.SessionKey, "/review")
	var instructions *string
	if value, ok := apputil.FormValueString(action.FormValue, "instructions"); ok {
		instructions = &value
	}
	target, err := s.app.UseCase.SubmitForm(s.app.Context(), id, reviewapp.Input{SessionKey: pending.SessionKey, UserID: action.UserID, ChatID: msg.ChatID, ChatType: msg.ChatType, MessageID: msg.MessageID, RootMessageID: msg.RootMessageID}, reviewapp.Workspace{ID: ws.ID, CWD: ws.Cwd}, instructions)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已启动 review"}, Card: rawCard(s.app.ReviewRenderer().SimpleStatusCard("Review 已启动", "blue", appreview.ConfirmationText(target), nil))}, nil
}
func (s ReviewFormService) reviewPendingForAction(action *feishu.CardAction, mode string) (*state.PendingRequest, ReviewPendingPayload, *callback.CardActionTriggerResponse) {
	if action == nil {
		return nil, ReviewPendingPayload{}, &callback.CardActionTriggerResponse{}
	}
	pending, payload, err := s.app.UseCase.Form(s.app.ReviewActionStringValue(action, "request_id"), action.UserID, mode)
	if err != nil {
		return nil, payload, &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}
	}
	return pending, payload, nil
}

// ---------------------------------------------------------------------------
// Local helpers (not exported)
// ---------------------------------------------------------------------------

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

func (d Dependencies) WorkspaceSelection() workspace.SelectionService {
	if d.ConfigProvider == nil {
		return workspace.SelectionService{}
	}
	return d.ConfigProvider.WorkspaceSelection()
}
