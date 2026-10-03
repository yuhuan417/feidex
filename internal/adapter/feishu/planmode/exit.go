package planmode

import (
	"context"
	"encoding/json"
	interactionapp "feidex/internal/application/interaction"
	domainsubmission "feidex/internal/domain/submission"
	"fmt"
	"log/slog"
	"strings"

	planapp "feidex/internal/application/plan"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	ExitPendingKind            = "codex_exit_plan_mode"
	ExitImplementCurrentAction = "codex_plan_mode.implement_current"
	ExitImplementFreshAction   = "codex_plan_mode.implement_fresh"
	ExitStayAction             = "codex_plan_mode.stay"
	ExitPendingTitle           = "Implement this plan?"
	ExitExpiredTitle           = "Plan confirmation expired"
	ExitFollowupKind           = "turn_followup"
)

type ExitPayload struct {
	PlanMarkdown string `json:"plan_markdown"`
}

type TurnStreamFlushResult struct {
	ShouldUsePlanExitPrompt bool
	PlanMarkdown            string
	PlanMessageID           string
}

func ExitContentCardTitle(a Dependencies, sessionKey, workspaceID, title string) string {
	return ContentCardTitleForSession(a, sessionKey, workspaceID, title)
}

func ExitPayloadFromPending(pending *state.PendingRequest) ExitPayload {
	var payload ExitPayload
	if pending == nil || strings.TrimSpace(pending.PayloadJSON) == "" {
		return payload
	}
	_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	return payload
}

func ExitPromptButtons(requestID string) []feishu.Button {
	return []feishu.Button{
		{
			Text: "Yes, implement this plan",
			Type: "primary",
			Value: map[string]any{
				"action":     ExitImplementCurrentAction,
				"request_id": requestID,
			},
		},
		{
			Text: "Yes, clear context and implement",
			Type: "default",
			Value: map[string]any{
				"action":     ExitImplementFreshAction,
				"request_id": requestID,
			},
		},
		{
			Text: "No, stay in Plan mode",
			Type: "default",
			Value: map[string]any{
				"action":     ExitStayAction,
				"request_id": requestID,
			},
		},
	}
}

func ExitPromptCard(a Dependencies, sessionKey, workspaceID, planMarkdown, requestID string) map[string]any {
	body := strings.TrimSpace(planMarkdown)
	if body == "" {
		body = "Plan mode has finished."
	}
	if a.ConfigProvider == nil || a.Renderer() == nil {
		return nil
	}
	return a.Renderer().SimpleStatusCard(ExitContentCardTitle(a, sessionKey, workspaceID, ExitPendingTitle), "orange", body, ExitPromptButtons(requestID))
}

func ExitSuccessCard(a Dependencies, sessionKey, workspaceID, title, body string) map[string]any {
	if a.ConfigProvider == nil || a.Renderer() == nil {
		return nil
	}
	return a.Renderer().SimpleStatusCard(ExitContentCardTitle(a, sessionKey, workspaceID, strings.TrimSpace(firstNonEmpty(title, ExitPendingTitle))), "green", strings.TrimSpace(body), nil)
}

func ExitFailureCard(a Dependencies, sessionKey, workspaceID, body string) map[string]any {
	if a.ConfigProvider == nil || a.Renderer() == nil {
		return nil
	}
	return a.Renderer().SimpleStatusCard(ExitContentCardTitle(a, sessionKey, workspaceID, ExitPendingTitle), "red", strings.TrimSpace(firstNonEmpty(body, "Unable to process the plan confirmation.")), nil)
}

func ExitExpiredCard(a Dependencies, sessionKey, workspaceID, body string) map[string]any {
	if a.ConfigProvider == nil || a.Renderer() == nil {
		return nil
	}
	return a.Renderer().SimpleStatusCard(ExitContentCardTitle(a, sessionKey, workspaceID, ExitExpiredTitle), "grey", strings.TrimSpace(firstNonEmpty(body, "This confirmation is no longer valid.")), nil)
}

func ExitPendingRequest(a Dependencies, sessionKey string) *state.PendingRequest {
	return a.UseCase.Confirmation(sessionKey)
}
func InvalidateCodexPlanModeExitArtifactsForSession(a Dependencies, sessionKey, reason string) {
	pending, err := a.UseCase.Invalidate(strings.TrimSpace(sessionKey))
	if err != nil {
		slog.Warn("plan confirmation invalidation failed", "error", err)
		return
	}
	if pending != nil && pending.FeishuMsgID != "" {
		_ = a.OutboundCapability().PatchCard(a.Context(), pending.FeishuMsgID, ExitExpiredCard(a, sessionKey, "", firstNonEmpty(reason, "This confirmation is no longer valid.")))
	}
}
func ProcessCodexPlanModeExitOnTurnCompleted(a Dependencies, sessionKey string, sub *domainsubmission.Submission, threadID, turnID, status string, flush TurnStreamFlushResult) bool {
	if a.ConfigProvider == nil || sub == nil || configuredBackend(a) != BackendCodex {
		return false
	}
	offered, err := a.UseCase.Offer(a.Context(), planapp.ConfirmationInput{SessionKey: sessionKey, ThreadID: threadID, TurnID: turnID, Status: status, Markdown: flush.PlanMarkdown, Eligible: flush.ShouldUsePlanExitPrompt, Submission: sub}, confirmationPresenter{deps: a, sub: sub, reuseMessageID: flush.PlanMessageID})
	if err != nil {
		slog.Warn("codex plan mode exit prompt delivery failed", "session_key", sessionKey, "submission_id", sub.ID, "error", err)
	}
	return offered
}

type confirmationPresenter struct {
	deps           Dependencies
	sub            *domainsubmission.Submission
	reuseMessageID string
}

func (p confirmationPresenter) DeliverInteraction(ctx context.Context, input interactionapp.DeliveryInput) (string, error) {
	card := ExitPromptCard(p.deps, input.Request.SessionKey, p.sub.WorkspaceID, PlanMarkdownFromPending(&input.Request), input.Request.ID)
	if card == nil {
		return "", fmt.Errorf("plan mode exit prompt unavailable")
	}
	if id := strings.TrimSpace(p.reuseMessageID); id != "" {
		if err := p.deps.OutboundCapability().PatchCard(ctx, id, card); err == nil {
			return id, nil
		}
	}
	return p.deps.OutboundCapability().ReplyInteractionCard(ctx, input.Request.ID, p.sub.TriggerMessageID, card, p.deps.ReplyInThreadForSubmission(p.sub))
}

func CompleteCodexPlanModeExit(a Dependencies, action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	if a.ConfigProvider == nil || action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	requestID := strings.TrimSpace(a.ActionStringValue(action, "request_id"))
	if requestID == "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	pending := a.State().Pending(requestID)
	if pending == nil || pending.Kind != ExitPendingKind {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"},
		}, nil
	}
	if strings.TrimSpace(action.MessageID) == "" {
		resp, _, err := runCodexPlanModeExitAction(a, actionName, pending, action)
		return resp, err
	}
	a.RunAsync(func() {
		resp, followupSub, err := runCodexPlanModeExitAction(a, actionName, pending, action)
		card := callbackResponseCard(resp)
		if card == nil {
			errText := callbackResponseToastText(resp)
			if err != nil {
				errText = err.Error()
			}
			card = ExitFailureCard(a, pending.SessionKey, "", firstNonEmpty(strings.TrimSpace(errText), "Unable to process the plan confirmation."))
		}
		if card == nil {
			return
		}
		if sendErr := sendCodexPlanModeExitFollowupCard(a, pending, action, card, followupSub); sendErr != nil {
			slog.Warn("codex plan mode exit follow-up delivery failed",
				"session_key", pending.SessionKey,
				"request_id", pending.ID,
				"message_id", strings.TrimSpace(action.MessageID),
				"error", sendErr,
			)
		}
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "info", Content: "正在处理计划确认"},
	}, nil
}

func sendCodexPlanModeExitFollowupCard(a Dependencies, pending *state.PendingRequest, action *feishu.CardAction, card map[string]any, sub *domainsubmission.Submission) error {
	if a.ConfigProvider == nil || a.OutboundCapability() == nil || pending == nil || card == nil {
		return fmt.Errorf("plan mode exit follow-up unavailable")
	}
	messageID := strings.TrimSpace(pending.FeishuMsgID)
	if messageID == "" && action != nil {
		messageID = strings.TrimSpace(action.MessageID)
	}
	if messageID == "" {
		return fmt.Errorf("plan mode exit follow-up message missing")
	}
	replyInThread := false
	if sub != nil {
		replyInThread = a.ReplyInThreadForSubmission(sub)
	} else if sess := a.State().Session(strings.TrimSpace(pending.SessionKey)); sess != nil {
		replyInThread = sess.ChatType == "group" && a.ReplyInThreadEnabled(sess.ChatType)
	}
	_, err := a.SendLocalTurnFollowupCard(a.Context(), messageID, card, replyInThread, sub, ExitFollowupKind)
	return err
}

func runCodexPlanModeExitAction(a Dependencies, actionName string, pending *state.PendingRequest, action *feishu.CardAction) (*callback.CardActionTriggerResponse, *domainsubmission.Submission, error) {
	if a.ConfigProvider == nil || pending == nil || action == nil {
		return nil, nil, fmt.Errorf("plan confirmation unavailable")
	}
	current, err := a.UseCase.Authorize(pending.ID, action.UserID)
	if err != nil {
		return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: err.Error()}}, nil, nil
	}

	switch strings.TrimSpace(actionName) {
	case ExitStayAction:
		return codexPlanModeExitStay(a, current)
	case ExitImplementFreshAction:
		return codexPlanModeExitImplementFresh(a, current)
	case ExitImplementCurrentAction:
		return codexPlanModeExitImplementCurrent(a, current)
	default:
		return nil, nil, fmt.Errorf("unsupported plan mode exit action %q", actionName)
	}
}

func codexPlanModeExitImplementCurrent(a Dependencies, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, *domainsubmission.Submission, error) {
	return renderImplementation(a, pending, false)
}
func codexPlanModeExitImplementFresh(a Dependencies, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, *domainsubmission.Submission, error) {
	return renderImplementation(a, pending, true)
}
func renderImplementation(a Dependencies, pending *state.PendingRequest, fresh bool) (*callback.CardActionTriggerResponse, *domainsubmission.Submission, error) {
	result, err := a.UseCase.Implement(a.Context(), pending, fresh)
	if err != nil {
		return nil, nil, err
	}
	title, body, toast := "Plan implementation started", "Submitted `Implement the plan.` to the current thread.", "已提交实现指令"
	if fresh {
		title, body, toast = "Fresh thread started", "Started a fresh thread and submitted the plan as a new submission.", "已在新 thread 提交实现指令"
		if result.ThreadID != "" {
			body += "\n\nthread: `" + result.ThreadID + "`"
		}
	}
	if result.Submission != nil && result.Submission.ID != "" {
		body += "\n\nsubmission: `" + result.Submission.ID + "`"
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: toast}, Card: rawCard(ExitSuccessCard(a, pending.SessionKey, "", title, body))}, result.Submission, nil
}
func codexPlanModeExitStay(a Dependencies, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, *domainsubmission.Submission, error) {
	if pending == nil {
		return nil, nil, fmt.Errorf("plan confirmation unavailable")
	}
	if err := a.UseCase.Stay(pending.ID); err != nil {
		return nil, nil, err
	}
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "success", Content: "已保持 plan mode"}, Card: rawCard(ExitSuccessCard(a, pending.SessionKey, "", "Plan mode kept", "Stayed in Plan mode."))}, nil, nil
}
func ClearCodexPlanModeForSession(a Dependencies, key string) (bool, error) {
	return a.UseCase.Clear(strings.TrimSpace(key))
}

func PlanMarkdownFromPending(pending *state.PendingRequest) string {
	if pending == nil {
		return ""
	}
	return strings.TrimSpace(ExitPayloadFromPending(pending).PlanMarkdown)
}

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

func callbackResponseCard(resp *callback.CardActionTriggerResponse) map[string]any {
	if resp == nil || resp.Card == nil {
		return nil
	}
	card, _ := resp.Card.Data.(map[string]any)
	return card
}

func callbackResponseToastText(resp *callback.CardActionTriggerResponse) string {
	if resp == nil || resp.Toast == nil {
		return ""
	}
	return strings.TrimSpace(resp.Toast.Content)
}
