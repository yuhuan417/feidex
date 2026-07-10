package planmode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/app/appcore"
	"feidex/internal/app/lifecycle"
	appsubmission "feidex/internal/app/submission"
	"feidex/internal/config"
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

func ExitContentCardTitle(a App, sessionKey, workspaceID, title string) string {
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

func ExitPromptCard(a App, sessionKey, workspaceID, planMarkdown, requestID string) map[string]any {
	body := strings.TrimSpace(planMarkdown)
	if body == "" {
		body = "Plan mode has finished."
	}
	if a == nil || a.Feishu() == nil {
		return nil
	}
	return a.Feishu().SimpleStatusCard(ExitContentCardTitle(a, sessionKey, workspaceID, ExitPendingTitle), "orange", body, ExitPromptButtons(requestID))
}

func ExitSuccessCard(a App, sessionKey, workspaceID, title, body string) map[string]any {
	if a == nil || a.Feishu() == nil {
		return nil
	}
	return a.Feishu().SimpleStatusCard(ExitContentCardTitle(a, sessionKey, workspaceID, strings.TrimSpace(appcore.FirstNonEmpty(title, ExitPendingTitle))), "green", strings.TrimSpace(body), nil)
}

func ExitFailureCard(a App, sessionKey, workspaceID, body string) map[string]any {
	if a == nil || a.Feishu() == nil {
		return nil
	}
	return a.Feishu().SimpleStatusCard(ExitContentCardTitle(a, sessionKey, workspaceID, ExitPendingTitle), "red", strings.TrimSpace(appcore.FirstNonEmpty(body, "Unable to process the plan confirmation.")), nil)
}

func ExitExpiredCard(a App, sessionKey, workspaceID, body string) map[string]any {
	if a == nil || a.Feishu() == nil {
		return nil
	}
	return a.Feishu().SimpleStatusCard(ExitContentCardTitle(a, sessionKey, workspaceID, ExitExpiredTitle), "grey", strings.TrimSpace(appcore.FirstNonEmpty(body, "This confirmation is no longer valid.")), nil)
}

func ExitPendingRequest(a App, sessionKey string) *state.PendingRequest {
	if a == nil {
		return nil
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	var latest *state.PendingRequest
	for _, req := range a.State().PendingRequests() {
		if req == nil || req.Kind != ExitPendingKind || !lifecycle.IsPendingRequestOpen(req) {
			continue
		}
		if strings.TrimSpace(req.SessionKey) != sessionKey {
			continue
		}
		if latest == nil || req.CreatedAt > latest.CreatedAt {
			latest = req
		}
	}
	return latest
}

func ExitOtherOpenPendingExists(a App, sessionKey, excludeID string) bool {
	if a == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	excludeID = strings.TrimSpace(excludeID)
	if sessionKey == "" {
		return false
	}
	for _, req := range a.State().PendingRequests() {
		if req == nil || !lifecycle.IsPendingRequestOpen(req) {
			continue
		}
		if strings.TrimSpace(req.SessionKey) != sessionKey {
			continue
		}
		if excludeID != "" && strings.TrimSpace(req.ID) == excludeID {
			continue
		}
		return true
	}
	return false
}

func SessionHasPlanExitBlockers(a App, sess *state.Session) bool {
	if sess == nil {
		return true
	}
	if a != nil && a.SessionHasActiveWork(sess) {
		return true
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return true
	}
	if state.NormalizeSessionStatus(appcore.FirstNonEmpty(strings.TrimSpace(sess.Status), state.SessionStatusIdle.String())) != state.SessionStatusIdle {
		return true
	}
	return false
}

func InvalidateCodexPlanModeExitArtifactsForSession(a App, sessionKey, reason string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || a == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	pending := ExitPendingRequest(a, sessionKey)
	if pending == nil {
		return
	}
	_ = a.State().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusExpired.String()
	})
	if pending.FeishuMsgID != "" {
		body := appcore.FirstNonEmpty(reason, "This confirmation is no longer valid.")
		_ = a.Feishu().PatchCard(context.Background(), pending.FeishuMsgID, ExitExpiredCard(a, sessionKey, "", body))
	}
}

func ProcessCodexPlanModeExitOnTurnCompleted(a App, sessionKey string, sub *state.Submission, threadID, turnID, status string, flush TurnStreamFlushResult) bool {
	if a == nil || sub == nil {
		return false
	}
	if appcore.ConfiguredBackend(a) != BackendCodex {
		return false
	}
	if strings.TrimSpace(status) != state.SubmissionStatusCompleted.String() {
		return false
	}
	if !flush.ShouldUsePlanExitPrompt {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = strings.TrimSpace(sub.SessionKey)
	}
	planMarkdown := strings.TrimSpace(flush.PlanMarkdown)
	if sessionKey == "" || planMarkdown == "" {
		return false
	}
	threadID = strings.TrimSpace(appcore.FirstNonEmpty(threadID, sub.ThreadID))
	turnID = strings.TrimSpace(appcore.FirstNonEmpty(turnID, sub.TurnID))
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != threadID {
		return false
	}
	if SessionHasPlanExitBlockers(a, sess) {
		return false
	}
	if open := ExitPendingRequest(a, sessionKey); open != nil {
		return false
	}
	// Product rule: plan-exit confirmation is only offered at the terminal
	// turn boundary. If another pending interaction exists now, do not defer
	// and do not re-surface this plan later.
	if ExitOtherOpenPendingExists(a, sessionKey, "") {
		return false
	}
	if err := sendCodexPlanModeExitPrompt(a, sub, planMarkdown, strings.TrimSpace(flush.PlanMessageID)); err != nil {
		slog.Warn("codex plan mode exit prompt delivery failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
		return false
	}
	return true
}

func sendCodexPlanModeExitPrompt(a App, sub *state.Submission, planMarkdown, reuseMessageID string) error {
	if a == nil || a.Feishu() == nil || sub == nil {
		return fmt.Errorf("plan mode exit prompt unavailable")
	}
	requestID, err := a.State().NextLocalID("codex-plan-exit")
	if err != nil {
		return err
	}
	card := ExitPromptCard(a, strings.TrimSpace(sub.SessionKey), strings.TrimSpace(sub.WorkspaceID), planMarkdown, requestID)
	if card == nil {
		return fmt.Errorf("plan mode exit prompt unavailable")
	}
	msgID := ""
	reuseMessageID = strings.TrimSpace(reuseMessageID)
	if reuseMessageID != "" {
		if err := a.Feishu().PatchCard(context.Background(), reuseMessageID, card); err == nil {
			msgID = reuseMessageID
		}
	}
	if msgID == "" {
		msgID, err = a.Feishu().ReplyCard(context.Background(), sub.TriggerMessageID, card, a.ReplyInThreadForSubmission(sub))
		if err != nil {
			return err
		}
	}
	payload := ExitPayload{PlanMarkdown: strings.TrimSpace(planMarkdown)}
	return a.State().SavePending(&state.PendingRequest{
		ID:         requestID,
		Kind:       ExitPendingKind,
		SessionKey: strings.TrimSpace(sub.SessionKey),
		ThreadID:   strings.TrimSpace(sub.ThreadID),
		// This is a post-turn local chooser, not server-request state for the
		// completed turn. Keeping TurnID empty prevents turn runtime cleanup
		// from deleting it immediately after delivery.
		TurnID:      "",
		OwnerUserID: strings.TrimSpace(sub.UserID),
		FeishuMsgID: msgID,
		PayloadJSON: appcore.MustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(30 * time.Minute).Unix(),
	})
}

func CompleteCodexPlanModeExit(a App, action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	if a == nil || action == nil {
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
			card = ExitFailureCard(a, pending.SessionKey, "", appcore.FirstNonEmpty(strings.TrimSpace(errText), "Unable to process the plan confirmation."))
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

func sendCodexPlanModeExitFollowupCard(a App, pending *state.PendingRequest, action *feishu.CardAction, card map[string]any, sub *state.Submission) error {
	if a == nil || a.Feishu() == nil || pending == nil || card == nil {
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
	_, err := a.SendLocalTurnFollowupCard(context.Background(), messageID, card, replyInThread, sub, ExitFollowupKind)
	return err
}

func runCodexPlanModeExitAction(a App, actionName string, pending *state.PendingRequest, action *feishu.CardAction) (*callback.CardActionTriggerResponse, *state.Submission, error) {
	if a == nil || pending == nil || action == nil {
		return nil, nil, fmt.Errorf("plan confirmation unavailable")
	}
	current := a.State().Pending(pending.ID)
	if current == nil || current.Kind != ExitPendingKind || state.NormalizePendingRequestStatus(current.Status) != state.PendingRequestStatusPending {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil, nil
	}
	if current.OwnerUserID != "" && current.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"},
		}, nil, nil
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

func codexPlanModeExitImplementCurrent(a App, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, *state.Submission, error) {
	if a == nil || pending == nil {
		return nil, nil, fmt.Errorf("plan confirmation unavailable")
	}
	sess := a.State().Session(pending.SessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil, nil
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(pending.ThreadID) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil, nil
	}
	if a.SessionHasActiveWork(sess) || len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前还有其他任务在处理，请先完成它们"},
		}, nil, nil
	}
	planModeCleared, err := ClearCodexPlanModeForSession(a, pending.SessionKey)
	if err != nil {
		return nil, nil, err
	}
	if !planModeCleared {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil, nil
	}
	sub, err := createCodexPlanModeExitSubmission(a, pending, "Implement the plan.")
	if err != nil {
		return nil, nil, err
	}
	if err := a.StartNextSubmission(pending.SessionKey); err != nil {
		return nil, nil, err
	}
	startedSub := a.State().Submission(sub.ID)
	if startedSub == nil {
		startedSub = sub
	}
	_ = a.State().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
	})
	body := "Submitted `Implement the plan.` to the current thread."
	if startedSub != nil && strings.TrimSpace(startedSub.ID) != "" {
		body = body + "\n\nsubmission: `" + startedSub.ID + "`"
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交实现指令"},
		Card:  rawCard(ExitSuccessCard(a, pending.SessionKey, "", "Plan implementation started", body)),
	}, startedSub, nil
}

func codexPlanModeExitImplementFresh(a App, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, *state.Submission, error) {
	if a == nil || pending == nil {
		return nil, nil, fmt.Errorf("plan confirmation unavailable")
	}
	sess := a.State().Session(pending.SessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil, nil
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(pending.ThreadID) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil, nil
	}
	if a.SessionHasActiveWork(sess) || len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前还有其他任务在处理，请先完成它们"},
		}, nil, nil
	}
	wsID := appcore.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), appcore.DefaultWorkspaceID(a))
	ws := firstNonEmptyWorkspace(a, wsID)
	if ws == nil {
		return nil, nil, fmt.Errorf("workspace %q not found", wsID)
	}
	binding, err := a.StartWorkspaceThread(pending.SessionKey, sess, ws)
	if err != nil {
		return nil, nil, err
	}
	planModeCleared, err := ClearCodexPlanModeForSession(a, pending.SessionKey)
	if err != nil {
		return nil, nil, err
	}
	if !planModeCleared {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil, nil
	}
	prompt := FreshPrompt(strings.TrimSpace(PlanMarkdownFromPending(pending)))
	sub, err := createCodexPlanModeExitSubmission(a, pending, prompt)
	if err != nil {
		return nil, nil, err
	}
	if err := a.StartNextSubmission(pending.SessionKey); err != nil {
		return nil, nil, err
	}
	startedSub := a.State().Submission(sub.ID)
	if startedSub == nil {
		startedSub = sub
	}
	_ = a.State().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
	})
	body := "Started a fresh thread and submitted the plan as a new submission."
	if binding != nil && strings.TrimSpace(binding.ThreadID) != "" {
		body += "\n\nthread: `" + binding.ThreadID + "`"
	}
	if startedSub != nil && strings.TrimSpace(startedSub.ID) != "" {
		body += "\nsubmission: `" + startedSub.ID + "`"
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已在新 thread 提交实现指令"},
		Card:  rawCard(ExitSuccessCard(a, pending.SessionKey, "", "Fresh thread started", body)),
	}, startedSub, nil
}

func codexPlanModeExitStay(a App, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, *state.Submission, error) {
	if a == nil || pending == nil {
		return nil, nil, fmt.Errorf("plan confirmation unavailable")
	}
	if current := a.State().Pending(pending.ID); current == nil || state.NormalizePendingRequestStatus(current.Status) != state.PendingRequestStatusPending {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil, nil
	}
	_ = a.State().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已保持 plan mode"},
		Card:  rawCard(ExitSuccessCard(a, pending.SessionKey, "", "Plan mode kept", "Stayed in Plan mode.")),
	}, nil, nil
}

func createCodexPlanModeExitSubmission(a App, pending *state.PendingRequest, inputText string) (*state.Submission, error) {
	if a == nil || pending == nil {
		return nil, fmt.Errorf("plan confirmation unavailable")
	}
	sess := a.State().Session(pending.SessionKey)
	if sess == nil {
		return nil, fmt.Errorf("session not found")
	}
	sub := &state.Submission{
		SessionKey:           strings.TrimSpace(pending.SessionKey),
		WorkspaceID:          appcore.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), appcore.DefaultWorkspaceID(a)),
		ThreadID:             strings.TrimSpace(sess.ActiveThreadID),
		UserID:               strings.TrimSpace(appcore.FirstNonEmpty(pending.OwnerUserID, sess.OwnerUserID)),
		ChatID:               strings.TrimSpace(sess.ChatID),
		TriggerMessageID:     strings.TrimSpace(appcore.FirstNonEmpty(pending.FeishuMsgID, sess.RootMessageID)),
		SourceMessageIDs:     appsubmission.UniqueStrings([]string{strings.TrimSpace(appcore.FirstNonEmpty(pending.FeishuMsgID, sess.RootMessageID))}),
		SourceRootMessageIDs: appsubmission.UniqueStrings([]string{strings.TrimSpace(appcore.FirstNonEmpty(sess.RootMessageID, pending.FeishuMsgID))}),
		InputText:            strings.TrimSpace(inputText),
		Status:               state.SubmissionStatusQueued.String(),
	}
	id, err := a.State().CreateSubmission(sub)
	if err != nil {
		return nil, err
	}
	sub.ID = id
	if err := a.State().QueueSubmission(pending.SessionKey, id); err != nil {
		return nil, err
	}
	return sub, nil
}

func ClearCodexPlanModeForSession(a App, sessionKey string) (bool, error) {
	if a == nil {
		return false, fmt.Errorf("app not initialized")
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return false, nil
	}
	sess := a.State().Session(sessionKey)
	if sess == nil {
		return false, nil
	}
	defaultMode, err := ResolveDefaultCodexCollaborationModeForSession(a, sess)
	if err != nil {
		return false, err
	}
	_, err = a.State().UpdateSession(sessionKey, func(sess *state.Session) {
		if sess == nil {
			return
		}
		sess.ActiveThreadCollaborationMode = defaultMode
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

func firstNonEmptyWorkspace(a App, workspaceID string) *config.Workspace {
	if a == nil {
		return nil
	}
	return config.FindWorkspace(a.Config(), workspaceID)
}

func FreshPrompt(planMarkdown string) string {
	planMarkdown = strings.TrimSpace(planMarkdown)
	intro := "A previous agent produced the plan below to accomplish the user's task. Implement the plan in a fresh context. Treat the plan as the source of user intent, re-read files as needed, and carry the work through implementation and verification."
	if planMarkdown == "" {
		return intro
	}
	return intro + "\n\n" + planMarkdown
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
