// Package claudesupport contains helper functions extracted from
// the app package for Claude approval, permission updates, plan mode
// bodies, user-input answer handling, and card delivery orchestration.
package claudesupport

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appapproval "feidex/internal/app/approval"
	"feidex/internal/app/apputil"
	"feidex/internal/app/cardactions"
	appclauderuntime "feidex/internal/app/clauderuntime"
	"feidex/internal/app/pendingforms"
	appruntime "feidex/internal/app/runtime"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------- small local helpers (keep unexported) ----------

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func stringValue(v any) string { return apputil.StringValue(v) }

func firstNonEmptyValue(values ...any) any {
	for _, value := range values {
		switch x := value.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(x) != "" {
				return value
			}
		case []any:
			if len(x) > 0 {
				return value
			}
		case map[string]any:
			if len(x) > 0 {
				return value
			}
		default:
			return value
		}
	}
	return nil
}

// normalizePermissionModeValue normalizes a Claude permission mode string
// using the canonical appruntime constants.
func normalizePermissionModeValue(value string) string {
	switch strings.TrimSpace(value) {
	case "", "default":
		return string(appruntime.ClaudePermissionModeDefault)
	case string(appruntime.ClaudePermissionModeAcceptEdits):
		return string(appruntime.ClaudePermissionModeAcceptEdits)
	case string(appruntime.ClaudePermissionModeBypass):
		return string(appruntime.ClaudePermissionModeBypass)
	case string(appruntime.ClaudePermissionModePlan):
		return string(appruntime.ClaudePermissionModePlan)
	default:
		return strings.TrimSpace(value)
	}
}

// ---------- callback types for app/ dependencies ----------

// DeliverPendingCardFunc delivers a card and creates a pending request record.
type DeliverPendingCardFunc func(sub *state.Submission, card map[string]any, reqKey, reqIDStored, backend, kind, sessionKey, threadID, turnID, itemID, ownerUserID, payloadJSON, waitingStatus, linkKind string, ttl time.Duration) error

// RenderApprovalCardFunc renders an approval card.
type RenderApprovalCardFunc func(sub *state.Submission, title, color, body string, buttons []feishu.Button) map[string]any

// SimpleStatusCardFunc creates a simple status card.
type SimpleStatusCardFunc func(title, color, body string, buttons []feishu.Button) map[string]any

// PatchCardFunc patches an existing card by message ID.
type PatchCardFunc func(messageID string, card map[string]any) error

// PrepareMentionTextFunc prepends attention mention to text.
type PrepareMentionTextFunc func(text, userID string) string

// RenderFormCardFunc renders a tool user input form card.
type RenderFormCardFunc func(requestID string, payload pendingforms.ToolUserInputPayload, drafts pendingforms.FormDrafts, ownerUserID string) map[string]any

// ResolvePlanFeedbackFunc resolves plan feedback for a pending request.
type ResolvePlanFeedbackFunc func(pendingID, feedback string) error

// FinalizePendingReplyFunc finalizes a pending reply. The return value
// is the finalized pending request (may be nil).
type FinalizePendingReplyFunc func(pending *state.PendingRequest) *state.PendingRequest

// CancelPendingFunc cancels a pending request.
type CancelPendingFunc func(pending *state.PendingRequest) error

// RawCardFunc converts a card map to a callback.Card.
type RawCardFunc func(card map[string]any) *callback.Card

// PendingLookupFunc looks up a pending request by ID.
type PendingLookupFunc func(requestID string) *state.PendingRequest

// ---------- Service ----------

// Service manages Claude support operations with callbacks for app/
// dependencies.
type Service struct {
	// Card delivery
	DeliverPendingCard DeliverPendingCardFunc
	RenderApprovalCard RenderApprovalCardFunc
	SimpleStatusCard   SimpleStatusCardFunc
	PatchCard          PatchCardFunc
	PrepareMentionText PrepareMentionTextFunc
	RenderFormCard     RenderFormCardFunc
	BackendClaude      string
	// Plan mode
	ResolvePlanFeedback  ResolvePlanFeedbackFunc
	FinalizePendingReply FinalizePendingReplyFunc
	CancelPending        CancelPendingFunc
	RawCard              RawCardFunc
	PendingLookup        PendingLookupFunc
}

// ---------- exported pure helper functions ----------

// ClaudeRequestIDStored normalises a request ID for storage.
func ClaudeRequestIDStored(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ""
	}
	return mustJSON(requestID)
}

// ClaudeApprovalButtons returns the approval buttons for a given kind.
func ClaudeApprovalButtons(kind, requestKey, sessionActionLabel string) []feishu.Button {
	sessionActionLabel = strings.TrimSpace(sessionActionLabel)
	switch appapproval.NormalizeKind(kind) {
	case appapproval.KindCommand:
		buttons := []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: cardactions.RequestActionValue{Action: "approval.command.accept", RequestID: requestKey}.Map()},
		}
		if sessionActionLabel != "" {
			buttons = append(buttons, feishu.Button{Text: sessionActionLabel, Type: "default", Value: cardactions.RequestActionValue{Action: "approval.command.accept_session", RequestID: requestKey}.Map()})
		}
		buttons = append(buttons,
			feishu.Button{Text: "拒绝", Type: "danger", Value: cardactions.RequestActionValue{Action: "approval.command.decline", RequestID: requestKey}.Map()},
			feishu.Button{Text: "拒绝并中断", Type: "danger", Value: cardactions.RequestActionValue{Action: "approval.command.cancel", RequestID: requestKey}.Map()},
		)
		return buttons
	case appapproval.KindFile:
		buttons := []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: cardactions.RequestActionValue{Action: "approval.file.accept", RequestID: requestKey}.Map()},
		}
		if sessionActionLabel != "" {
			buttons = append(buttons, feishu.Button{Text: sessionActionLabel, Type: "default", Value: cardactions.RequestActionValue{Action: "approval.file.accept_session", RequestID: requestKey}.Map()})
		}
		buttons = append(buttons,
			feishu.Button{Text: "拒绝", Type: "danger", Value: cardactions.RequestActionValue{Action: "approval.file.decline", RequestID: requestKey}.Map()},
			feishu.Button{Text: "拒绝并中断", Type: "danger", Value: cardactions.RequestActionValue{Action: "approval.file.cancel", RequestID: requestKey}.Map()},
		)
		return buttons
	case appapproval.KindPermissions:
		buttons := []feishu.Button{
			{Text: "本次允许", Type: "primary", Value: cardactions.RequestActionValue{Action: "approval.permissions.accept_turn", RequestID: requestKey}.Map()},
		}
		if sessionActionLabel != "" {
			buttons = append(buttons, feishu.Button{Text: sessionActionLabel, Type: "default", Value: cardactions.RequestActionValue{Action: "approval.permissions.accept_session", RequestID: requestKey}.Map()})
		}
		buttons = append(buttons, feishu.Button{Text: "拒绝", Type: "danger", Value: cardactions.RequestActionValue{Action: "approval.permissions.decline", RequestID: requestKey}.Map()})
		return buttons
	default:
		buttons := []feishu.Button{
			{Text: "允许一次", Type: "primary", Value: cardactions.RequestActionValue{Action: "approval." + kind + ".accept", RequestID: requestKey}.Map()},
		}
		if sessionActionLabel != "" {
			buttons = append(buttons, feishu.Button{Text: sessionActionLabel, Type: "default", Value: cardactions.RequestActionValue{Action: "approval." + kind + ".accept_session", RequestID: requestKey}.Map()})
		}
		buttons = append(buttons, feishu.Button{Text: "拒绝", Type: "danger", Value: cardactions.RequestActionValue{Action: "approval." + kind + ".decline", RequestID: requestKey}.Map()})
		return buttons
	}
}

// SafeClaudeSessionPermissionUpdates filters and normalises permission
// update suggestions, returning only valid session-scoped updates.
func SafeClaudeSessionPermissionUpdates(suggestions []map[string]any) []map[string]any {
	return appclauderuntime.MapSessionPermissionUpdates(appclauderuntime.SafeClaudeSessionPermissionUpdates(suggestions))
}

// NormalizeClaudeSessionPermissionUpdate validates and normalises a single
// session permission update map.
func NormalizeClaudeSessionPermissionUpdate(update map[string]any) (map[string]any, bool) {
	normalized, ok := appclauderuntime.NormalizeSessionPermissionUpdate(update)
	if !ok {
		return nil, false
	}
	return normalized.Map(), true
}

// DescribeClaudeSessionPermissionUpdates returns a human-readable summary
// of a list of session permission updates.
func DescribeClaudeSessionPermissionUpdates(updates []map[string]any) string {
	return appclauderuntime.DescribeClaudeSessionPermissionUpdates(appclauderuntime.SafeClaudeSessionPermissionUpdates(updates))
}

// ClaudeApprovalResolutionForAction maps a card action name to the
// corresponding ClaudeApprovalResolution and an optional error string.
func ClaudeApprovalResolutionForAction(actionName string) (appruntime.ClaudeApprovalResolution, string) {
	switch strings.TrimSpace(actionName) {
	case "approval.command.accept", "approval.file.accept", "approval.permissions.accept_turn":
		return appruntime.ClaudeApprovalResolution{Behavior: "allow", Scope: "turn"}, ""
	case "approval.command.accept_session", "approval.file.accept_session", "approval.permissions.accept_session":
		return appruntime.ClaudeApprovalResolution{Behavior: "allow", Scope: "session"}, ""
	case "approval.command.cancel", "approval.file.cancel":
		return appruntime.ClaudeApprovalResolution{
			Behavior:  "deny",
			Message:   "Declined by user",
			Interrupt: true,
		}, ""
	case "approval.command.decline", "approval.file.decline", "approval.permissions.decline":
		return appruntime.ClaudeApprovalResolution{
			Behavior: "deny",
			Message:  "Declined by user",
		}, ""
	default:
		return appruntime.ClaudeApprovalResolution{}, "不支持的审批动作"
	}
}

// ClaudeAnswersFromSelections builds answer and summary maps from a
// tool-user-input payload and the user's raw selections.
func ClaudeAnswersFromSelections(payload pendingforms.ToolUserInputPayload, selections map[string]string) (map[string]string, string, error) {
	answers := map[string]string{}
	summaryLines := make([]string, 0, len(selections))
	for _, q := range payload.Questions {
		raw := strings.TrimSpace(selections[q.ID])
		if raw == "" {
			continue
		}
		value, summary, err := ClaudeQuestionAnswer(raw, q)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", q.ID, err)
		}
		answers[q.Question] = value
		summaryLines = append(summaryLines, fmt.Sprintf("`%s`: %s", q.ID, summary))
	}
	if len(answers) == 0 {
		return nil, "", fmt.Errorf("answer is required")
	}
	return answers, strings.Join(summaryLines, "\n"), nil
}

// ParseClaudeToolUserInputResponse parses a free-text response and
// resolves it against the tool-user-input payload.
func ParseClaudeToolUserInputResponse(text string, payload pendingforms.ToolUserInputPayload) (map[string]string, string, error) {
	answerMap := pendingforms.ParseStructuredLines(text)
	selections := make(map[string]string, len(payload.Questions))
	for _, q := range payload.Questions {
		raw := strings.TrimSpace(answerMap[q.ID])
		if raw == "" && len(payload.Questions) == 1 {
			raw = text
		}
		selections[q.ID] = raw
	}
	return ClaudeAnswersFromSelections(payload, selections)
}

// ClaudeQuestionAnswer resolves a raw answer string against a single
// ToolUserInputQuestion.
func ClaudeQuestionAnswer(raw string, q pendingforms.ToolUserInputQuestion) (string, string, error) {
	answers, err := pendingforms.ParseQuestionAnswers(raw, q)
	if err != nil {
		return "", "", err
	}
	return strings.Join(answers, ", "), pendingforms.SummarizeAnswers(answers, q.IsSecret), nil
}

// ClaudePlanSubmittedBody builds the card body text shown after a plan
// feedback submission.
func ClaudePlanSubmittedBody(pending *state.PendingRequest, feedback string) string {
	lines := []string{"已提交给 Claude，等待继续处理。"}
	if strings.TrimSpace(feedback) != "" {
		lines = append(lines, "", "你的反馈：", strings.TrimSpace(feedback))
	}
	if original := ClaudePlanOriginalBody(pending); original != "" {
		lines = append(lines, "", "原计划：", original)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ClaudePlanCancelledBody builds the card body text shown after plan
// cancellation.
func ClaudePlanCancelledBody(pending *state.PendingRequest) string {
	lines := []string{"已取消本次计划确认。"}
	if original := ClaudePlanOriginalBody(pending); original != "" {
		lines = append(lines, "", "原计划：", original)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ClaudePlanOriginalBody extracts the original plan body from a pending
// request's payload JSON.
func ClaudePlanOriginalBody(pending *state.PendingRequest) string {
	if pending == nil || strings.TrimSpace(pending.PayloadJSON) == "" {
		return ""
	}
	var payload struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(pending.PayloadJSON), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Body)
}

// ---------- Service methods — card delivery ----------

// SendApprovalCardWithPayload sends a Claude approval card with an optional
// request payload.
func (s *Service) SendApprovalCardWithPayload(sub *state.Submission, kind, requestID, sessionKey, threadID, turnID, itemID, body string, requestPayload map[string]any, sessionActionLabel string) error {
	if sub == nil {
		return fmt.Errorf("claude approval delivery unavailable")
	}
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}

	title := "等待审批"
	buttons := ClaudeApprovalButtons(kind, requestKey, sessionActionLabel)
	payload := appapproval.RequestPayload{
		Body:               strings.TrimSpace(body),
		Request:            requestPayload,
		SessionActionLabel: strings.TrimSpace(sessionActionLabel),
	}
	if appapproval.NormalizeKind(kind) == appapproval.KindPermissions {
		title = "权限请求"
		if permissions, ok := requestPayload["permissions"].(map[string]any); ok {
			payload.Permissions = appapproval.CloneJSONMap(permissions)
		}
	}

	card := s.RenderApprovalCard(sub, title, "orange", strings.TrimSpace(body), buttons)
	return s.DeliverPendingCard(sub, card,
		requestKey,
		ClaudeRequestIDStored(requestKey),
		s.BackendClaude,
		strings.TrimSpace(kind),
		strings.TrimSpace(sessionKey),
		strings.TrimSpace(threadID),
		strings.TrimSpace(turnID),
		strings.TrimSpace(itemID),
		strings.TrimSpace(sub.UserID),
		payload.MarshalJSONText(),
		state.SubmissionStatusWaitingApproval.String(),
		"approval_card",
		0,
	)
}

// SendUserInputCard sends a Claude user input question card.
func (s *Service) SendUserInputCard(sub *state.Submission, requestID, sessionKey string, payload pendingforms.ToolUserInputPayload) error {
	if sub == nil || len(payload.Questions) == 0 {
		return fmt.Errorf("claude question delivery unavailable")
	}
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}
	q := payload.Questions[0]
	buttons := make([]feishu.Button, 0, len(q.Options))
	for _, opt := range q.Options {
		buttons = append(buttons, feishu.Button{
			Text: opt.Label,
			Type: "default",
			Value: map[string]any{
				"action":      "user_input.answer",
				"request_id":  requestKey,
				"question_id": q.ID,
				"answer":      opt.Label,
			},
		})
	}
	card := s.SimpleStatusCard("需要补充输入", "orange", s.PrepareMentionText(q.Question, sub.UserID), buttons)
	return s.DeliverPendingCard(sub, card,
		requestKey,
		ClaudeRequestIDStored(requestKey),
		s.BackendClaude,
		"tool_request_user_input",
		strings.TrimSpace(sessionKey),
		payload.ThreadID,
		payload.TurnID,
		payload.ItemID,
		strings.TrimSpace(sub.UserID),
		mustJSON(payload),
		state.SubmissionStatusWaitingUserInput.String(),
		"user_input_card",
		0,
	)
}

// SendUserInputFormCard sends a Claude user input form card.
func (s *Service) SendUserInputFormCard(sub *state.Submission, requestID, sessionKey string, payload pendingforms.ToolUserInputPayload) error {
	if sub == nil {
		return fmt.Errorf("claude question delivery unavailable")
	}
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}
	card := s.RenderFormCard(requestKey, payload, pendingforms.FormDrafts{}, sub.UserID)
	return s.DeliverPendingCard(sub, card,
		requestKey,
		ClaudeRequestIDStored(requestKey),
		s.BackendClaude,
		"tool_request_user_input_form",
		strings.TrimSpace(sessionKey),
		payload.ThreadID,
		payload.TurnID,
		payload.ItemID,
		strings.TrimSpace(sub.UserID),
		mustJSON(payload),
		state.SubmissionStatusWaitingUserInput.String(),
		"user_input_card",
		0,
	)
}

// SendPlanModeCard sends a Claude plan mode confirmation card.
func (s *Service) SendPlanModeCard(sub *state.Submission, requestID, sessionKey, threadID, turnID, body string) error {
	if sub == nil {
		return fmt.Errorf("claude plan confirmation unavailable")
	}
	requestKey := strings.TrimSpace(requestID)
	if requestKey == "" {
		return fmt.Errorf("missing request id")
	}
	card := s.SimpleStatusCard("Claude 计划确认", "orange", s.PrepareMentionText(strings.TrimSpace(body), sub.UserID), []feishu.Button{
		{Text: "批准", Type: "primary", Value: map[string]any{"action": "pending_form.plan_approve", "request_id": requestKey}},
		{Text: "拒绝", Type: "danger", Value: map[string]any{"action": "pending_form.plan_reject", "request_id": requestKey}},
	})
	return s.DeliverPendingCard(sub, card,
		requestKey,
		ClaudeRequestIDStored(requestKey),
		s.BackendClaude,
		"claude_exit_plan_mode",
		strings.TrimSpace(sessionKey),
		strings.TrimSpace(threadID),
		strings.TrimSpace(turnID),
		requestKey,
		strings.TrimSpace(sub.UserID),
		mustJSON(map[string]any{"body": strings.TrimSpace(body)}),
		state.SubmissionStatusWaitingUserInput.String(),
		"claude_plan_card",
		0,
	)
}

// ---------- Service methods — plan mode completion ----------

// CompletePlanModeText completes a plan mode text submission.
func (s *Service) CompletePlanModeText(feedback string, pending *state.PendingRequest) error {
	if pending == nil {
		return nil
	}
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return fmt.Errorf("反馈不能为空")
	}
	if err := s.ResolvePlanFeedback(pending.ID, feedback); err != nil {
		return err
	}
	_ = s.FinalizePendingReply(pending)
	if pending.FeishuMsgID != "" {
		_ = s.PatchCard(pending.FeishuMsgID, s.SimpleStatusCard("计划反馈已提交", "green", ClaudePlanSubmittedBody(pending, feedback), nil))
	}
	return nil
}

// CardActionResult holds the result of a plan approve/reject action.
type CardActionResult struct {
	ToastContent string
	ToastType    string
	CardMap      map[string]any
}

// CompletePlanApprove handles the plan approve card action.
func (s *Service) CompletePlanApprove(requestID, userID string) (*CardActionResult, error) {
	pending := s.PendingLookup(requestID)
	if pending == nil {
		return &CardActionResult{ToastContent: "请求已过期", ToastType: "warning"}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != userID {
		return &CardActionResult{ToastContent: "你没有权限处理这个请求", ToastType: "warning"}, nil
	}
	if err := s.ResolvePlanFeedback(pending.ID, "Approve"); err != nil {
		return &CardActionResult{ToastContent: "提交失败，请重试", ToastType: "warning"}, nil
	}
	_ = s.FinalizePendingReply(pending)
	return &CardActionResult{
		ToastContent: "已批准",
		ToastType:    "success",
		CardMap:      s.SimpleStatusCard("计划已批准", "green", ClaudePlanSubmittedBody(pending, "Approve"), nil),
	}, nil
}

// CompletePlanReject handles the plan reject card action.
func (s *Service) CompletePlanReject(requestID, userID string, cancelFn func(pending *state.PendingRequest) error) (*CardActionResult, error) {
	pending := s.PendingLookup(requestID)
	if pending == nil {
		return &CardActionResult{ToastContent: "请求已过期", ToastType: "warning"}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != userID {
		return &CardActionResult{ToastContent: "你没有权限处理这个请求", ToastType: "warning"}, nil
	}
	if err := cancelFn(pending); err != nil {
		return &CardActionResult{ToastContent: "提交失败，请重试", ToastType: "warning"}, nil
	}
	_ = s.FinalizePendingReply(pending)
	return &CardActionResult{
		ToastContent: "已拒绝",
		ToastType:    "success",
		CardMap:      s.SimpleStatusCard("计划已拒绝", "grey", ClaudePlanCancelledBody(pending), nil),
	}, nil
}
