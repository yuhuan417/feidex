package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appsubmission "feidex/internal/app/submission"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

const (
	codexPlanModeExitPendingKind            = "codex_exit_plan_mode"
	codexPlanModeExitImplementCurrentAction = "codex_plan_mode.implement_current"
	codexPlanModeExitImplementFreshAction   = "codex_plan_mode.implement_fresh"
	codexPlanModeExitStayAction             = "codex_plan_mode.stay"
	codexPlanModeExitPendingTitle           = "Implement this plan?"
	codexPlanModeExitExpiredTitle           = "Plan confirmation expired"
	codexPlanModeExitProcessingTitle        = "Implement this plan?"
)

type codexPlanModeExitPayload struct {
	PlanMarkdown string `json:"plan_markdown"`
}

func codexPlanModeExitContentCardTitle(a *App, sessionKey, workspaceID, title string) string {
	return contentCardTitleForSession(a, sessionKey, workspaceID, title)
}

func codexPlanModeExitPayloadFromPending(pending *state.PendingRequest) codexPlanModeExitPayload {
	var payload codexPlanModeExitPayload
	if pending == nil || strings.TrimSpace(pending.PayloadJSON) == "" {
		return payload
	}
	_ = json.Unmarshal([]byte(pending.PayloadJSON), &payload)
	return payload
}

func codexPlanModeExitPromptButtons(requestID string) []feishu.Button {
	return []feishu.Button{
		{
			Text: "Yes, implement this plan",
			Type: "primary",
			Value: map[string]any{
				"action":     codexPlanModeExitImplementCurrentAction,
				"request_id": requestID,
			},
		},
		{
			Text: "Yes, clear context and implement",
			Type: "default",
			Value: map[string]any{
				"action":     codexPlanModeExitImplementFreshAction,
				"request_id": requestID,
			},
		},
		{
			Text: "No, stay in Plan mode",
			Type: "default",
			Value: map[string]any{
				"action":     codexPlanModeExitStayAction,
				"request_id": requestID,
			},
		},
	}
}

func codexPlanModeExitPromptCard(a *App, sessionKey, workspaceID, planMarkdown, requestID string) map[string]any {
	body := strings.TrimSpace(planMarkdown)
	if body == "" {
		body = "Plan mode has finished."
	}
	if a == nil || a.feishu == nil {
		return nil
	}
	return a.feishu.SimpleStatusCard(codexPlanModeExitContentCardTitle(a, sessionKey, workspaceID, codexPlanModeExitPendingTitle), "orange", body, codexPlanModeExitPromptButtons(requestID))
}

func codexPlanModeExitProcessingCard(a *App, sessionKey, workspaceID, body string) map[string]any {
	if a == nil || a.feishu == nil {
		return nil
	}
	return a.feishu.SimpleStatusCard(codexPlanModeExitContentCardTitle(a, sessionKey, workspaceID, codexPlanModeExitProcessingTitle), "blue", strings.TrimSpace(firstNonEmpty(body, "Processing your choice...")), nil)
}

func codexPlanModeExitSuccessCard(a *App, sessionKey, workspaceID, title, body string) map[string]any {
	if a == nil || a.feishu == nil {
		return nil
	}
	return a.feishu.SimpleStatusCard(codexPlanModeExitContentCardTitle(a, sessionKey, workspaceID, strings.TrimSpace(firstNonEmpty(title, codexPlanModeExitPendingTitle))), "green", strings.TrimSpace(body), nil)
}

func codexPlanModeExitFailureCard(a *App, sessionKey, workspaceID, body string) map[string]any {
	if a == nil || a.feishu == nil {
		return nil
	}
	return a.feishu.SimpleStatusCard(codexPlanModeExitContentCardTitle(a, sessionKey, workspaceID, codexPlanModeExitPendingTitle), "red", strings.TrimSpace(firstNonEmpty(body, "Unable to process the plan confirmation.")), nil)
}

func codexPlanModeExitExpiredCard(a *App, sessionKey, workspaceID, body string) map[string]any {
	if a == nil || a.feishu == nil {
		return nil
	}
	return a.feishu.SimpleStatusCard(codexPlanModeExitContentCardTitle(a, sessionKey, workspaceID, codexPlanModeExitExpiredTitle), "grey", strings.TrimSpace(firstNonEmpty(body, "This confirmation is no longer valid.")), nil)
}

func codexPlanModeExitPendingRequest(a *App, sessionKey string) *state.PendingRequest {
	if a == nil {
		return nil
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}
	var latest *state.PendingRequest
	for _, req := range a.State().PendingRequests() {
		if req == nil || req.Kind != codexPlanModeExitPendingKind || !isPendingRequestOpen(req) {
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

func codexPlanModeExitOtherOpenPendingExists(a *App, sessionKey, excludeID string) bool {
	if a == nil {
		return false
	}
	sessionKey = strings.TrimSpace(sessionKey)
	excludeID = strings.TrimSpace(excludeID)
	if sessionKey == "" {
		return false
	}
	for _, req := range a.State().PendingRequests() {
		if req == nil || !isPendingRequestOpen(req) {
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

func codexPlanModeExitSessionHasPlanExitBlockers(sess *state.Session) bool {
	if sess == nil {
		return true
	}
	if sessionHasActiveWork(sess) {
		return true
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return true
	}
	if state.NormalizeSessionStatus(firstNonEmpty(strings.TrimSpace(sess.Status), state.SessionStatusIdle.String())) != state.SessionStatusIdle {
		return true
	}
	return false
}

func invalidateCodexPlanModeExitArtifactsForSession(a *App, sessionKey, reason string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || a == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	pending := codexPlanModeExitPendingRequest(a, sessionKey)
	if pending == nil {
		return
	}
	_ = a.State().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusExpired.String()
	})
	if pending.FeishuMsgID != "" {
		body := firstNonEmpty(reason, "This confirmation is no longer valid.")
		_ = a.feishu.PatchCard(context.Background(), pending.FeishuMsgID, codexPlanModeExitExpiredCard(a, sessionKey, "", body))
	}
}

func processCodexPlanModeExitOnTurnCompleted(a *App, sessionKey string, sub *state.Submission, threadID, turnID, status string, flush turnStreamFlushResult) {
	if a == nil || sub == nil {
		return
	}
	if configuredBackend(a) != backendCodex {
		return
	}
	if strings.TrimSpace(status) != state.SubmissionStatusCompleted.String() {
		return
	}
	if !flush.SawPlanItem || !flush.PlanCompleted {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = strings.TrimSpace(sub.SessionKey)
	}
	planMarkdown := strings.TrimSpace(flush.PlanMarkdown)
	if sessionKey == "" || planMarkdown == "" {
		return
	}
	threadID = strings.TrimSpace(firstNonEmpty(threadID, sub.ThreadID))
	turnID = strings.TrimSpace(firstNonEmpty(turnID, sub.TurnID))
	sess := a.State().Session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveThreadID) != threadID {
		return
	}
	if codexPlanModeExitSessionHasPlanExitBlockers(sess) {
		return
	}
	if open := codexPlanModeExitPendingRequest(a, sessionKey); open != nil {
		return
	}
	// Product rule: plan-exit confirmation is only offered at the terminal
	// turn boundary. If another pending interaction exists now, do not defer
	// and do not re-surface this plan later.
	if codexPlanModeExitOtherOpenPendingExists(a, sessionKey, "") {
		return
	}
	if err := sendCodexPlanModeExitPrompt(a, sub, planMarkdown); err != nil {
		slog.Warn("codex plan mode exit prompt delivery failed",
			"session_key", sessionKey,
			"submission_id", sub.ID,
			"thread_id", threadID,
			"turn_id", turnID,
			"error", err,
		)
	}
}

func sendCodexPlanModeExitPrompt(a *App, sub *state.Submission, planMarkdown string) error {
	if a == nil || a.feishu == nil || sub == nil {
		return fmt.Errorf("plan mode exit prompt unavailable")
	}
	requestID, err := a.State().NextLocalID("codex-plan-exit")
	if err != nil {
		return err
	}
	card := codexPlanModeExitPromptCard(a, strings.TrimSpace(sub.SessionKey), strings.TrimSpace(sub.WorkspaceID), planMarkdown, requestID)
	if card == nil {
		return fmt.Errorf("plan mode exit prompt unavailable")
	}
	msgID, err := a.feishu.ReplyCard(context.Background(), sub.TriggerMessageID, card, replyInThreadForSubmission(a, sub))
	if err != nil {
		return err
	}
	payload := codexPlanModeExitPayload{PlanMarkdown: strings.TrimSpace(planMarkdown)}
	return a.State().SavePending(&state.PendingRequest{
		ID:         requestID,
		Kind:       codexPlanModeExitPendingKind,
		SessionKey: strings.TrimSpace(sub.SessionKey),
		ThreadID:   strings.TrimSpace(sub.ThreadID),
		// This is a post-turn local chooser, not server-request state for the
		// completed turn. Keeping TurnID empty prevents turn runtime cleanup
		// from deleting it immediately after delivery.
		TurnID:      "",
		OwnerUserID: strings.TrimSpace(sub.UserID),
		FeishuMsgID: msgID,
		PayloadJSON: mustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(30 * time.Minute).Unix(),
	})
}

func completeCodexPlanModeExit(a *App, action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	if a == nil || action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	requestID := strings.TrimSpace(actionStringValue(action, "request_id"))
	if requestID == "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	pending := a.State().Pending(requestID)
	if pending == nil || pending.Kind != codexPlanModeExitPendingKind {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	if pending.OwnerUserID != "" && pending.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"},
		}, nil
	}
	return completeAsyncRenderedCardAction(
		a,
		action,
		pending.SessionKey,
		"正在处理计划确认",
		codexPlanModeExitProcessingCard(a, pending.SessionKey, "", "正在处理你的选择，请稍候。"),
		func() (*callback.CardActionTriggerResponse, error) {
			return runCodexPlanModeExitAction(a, actionName, pending, action)
		},
		func(sessionKey, errText string) map[string]any {
			return codexPlanModeExitFailureCard(a, pending.SessionKey, "", firstNonEmpty(strings.TrimSpace(errText), "Unable to process the plan confirmation."))
		},
		"codex plan mode exit patch failed",
	)
}

func runCodexPlanModeExitAction(a *App, actionName string, pending *state.PendingRequest, action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	if a == nil || pending == nil || action == nil {
		return nil, fmt.Errorf("plan confirmation unavailable")
	}
	current := a.State().Pending(pending.ID)
	if current == nil || current.Kind != codexPlanModeExitPendingKind || state.NormalizePendingRequestStatus(current.Status) != state.PendingRequestStatusPending {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	if current.OwnerUserID != "" && current.OwnerUserID != action.UserID {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "你没有权限处理这个请求"},
		}, nil
	}
	switch strings.TrimSpace(actionName) {
	case codexPlanModeExitStayAction:
		return codexPlanModeExitStay(a, current)
	case codexPlanModeExitImplementFreshAction:
		return codexPlanModeExitImplementFresh(a, current)
	case codexPlanModeExitImplementCurrentAction:
		return codexPlanModeExitImplementCurrent(a, current)
	default:
		return nil, fmt.Errorf("unsupported plan mode exit action %q", actionName)
	}
}

func codexPlanModeExitImplementCurrent(a *App, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, error) {
	if a == nil || pending == nil {
		return nil, fmt.Errorf("plan confirmation unavailable")
	}
	sess := a.State().Session(pending.SessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(pending.ThreadID) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	if sessionHasActiveWork(sess) || len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前还有其他任务在处理，请先完成它们"},
		}, nil
	}
	planModeCleared, err := clearCodexPlanModeForSession(a, pending.SessionKey)
	if err != nil {
		return nil, err
	}
	if !planModeCleared {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	sub, err := createCodexPlanModeExitSubmission(a, pending, "Implement the plan.")
	if err != nil {
		return nil, err
	}
	if err := startNextSubmission(a, pending.SessionKey); err != nil {
		return nil, err
	}
	_ = a.State().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
	})
	body := "Submitted `Implement the plan.` to the current thread."
	if sub != nil && strings.TrimSpace(sub.ID) != "" {
		body = body + "\n\nsubmission: `" + sub.ID + "`"
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已提交实现指令"},
		Card:  rawCard(codexPlanModeExitSuccessCard(a, pending.SessionKey, "", "Plan implementation started", body)),
	}, nil
}

func codexPlanModeExitImplementFresh(a *App, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, error) {
	if a == nil || pending == nil {
		return nil, fmt.Errorf("plan confirmation unavailable")
	}
	sess := a.State().Session(pending.SessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	if strings.TrimSpace(sess.ActiveThreadID) != strings.TrimSpace(pending.ThreadID) {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	if sessionHasActiveWork(sess) || len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "当前还有其他任务在处理，请先完成它们"},
		}, nil
	}
	wsID := firstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), defaultWorkspaceID(a))
	ws := firstNonEmptyWorkspace(a, wsID)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found", wsID)
	}
	binding, err := conversationBackend(a).StartWorkspaceThread(pending.SessionKey, sess, ws)
	if err != nil {
		return nil, err
	}
	planModeCleared, err := clearCodexPlanModeForSession(a, pending.SessionKey)
	if err != nil {
		return nil, err
	}
	if !planModeCleared {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	prompt := codexPlanModeExitFreshPrompt(strings.TrimSpace(codexPlanModeExitPlanMarkdownFromPending(pending)))
	sub, err := createCodexPlanModeExitSubmission(a, pending, prompt)
	if err != nil {
		return nil, err
	}
	if err := startNextSubmission(a, pending.SessionKey); err != nil {
		return nil, err
	}
	_ = a.State().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
	})
	body := "Started a fresh thread and submitted the plan as a new submission."
	if binding != nil && strings.TrimSpace(binding.ThreadID) != "" {
		body += "\n\nthread: `" + binding.ThreadID + "`"
	}
	if sub != nil && strings.TrimSpace(sub.ID) != "" {
		body += "\nsubmission: `" + sub.ID + "`"
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已在新 thread 提交实现指令"},
		Card:  rawCard(codexPlanModeExitSuccessCard(a, pending.SessionKey, "", "Fresh thread started", body)),
	}, nil
}

func codexPlanModeExitStay(a *App, pending *state.PendingRequest) (*callback.CardActionTriggerResponse, error) {
	if a == nil || pending == nil {
		return nil, fmt.Errorf("plan confirmation unavailable")
	}
	if current := a.State().Pending(pending.ID); current == nil || state.NormalizePendingRequestStatus(current.Status) != state.PendingRequestStatusPending {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{Type: "warning", Content: "请求已过期"},
		}, nil
	}
	_ = a.State().UpdatePending(pending.ID, func(req *state.PendingRequest) {
		req.Status = state.PendingRequestStatusResolved.String()
	})
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "已保持 plan mode"},
		Card:  rawCard(codexPlanModeExitSuccessCard(a, pending.SessionKey, "", "Plan mode kept", "Stayed in Plan mode.")),
	}, nil
}

func createCodexPlanModeExitSubmission(a *App, pending *state.PendingRequest, inputText string) (*state.Submission, error) {
	if a == nil || pending == nil {
		return nil, fmt.Errorf("plan confirmation unavailable")
	}
	sess := a.State().Session(pending.SessionKey)
	if sess == nil {
		return nil, fmt.Errorf("session not found")
	}
	sub := &state.Submission{
		SessionKey:           strings.TrimSpace(pending.SessionKey),
		WorkspaceID:          firstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), defaultWorkspaceID(a)),
		ThreadID:             strings.TrimSpace(sess.ActiveThreadID),
		UserID:               strings.TrimSpace(firstNonEmpty(pending.OwnerUserID, sess.OwnerUserID)),
		ChatID:               strings.TrimSpace(sess.ChatID),
		TriggerMessageID:     strings.TrimSpace(firstNonEmpty(pending.FeishuMsgID, sess.RootMessageID)),
		SourceMessageIDs:     appsubmission.UniqueStrings([]string{strings.TrimSpace(firstNonEmpty(pending.FeishuMsgID, sess.RootMessageID))}),
		SourceRootMessageIDs: appsubmission.UniqueStrings([]string{strings.TrimSpace(firstNonEmpty(sess.RootMessageID, pending.FeishuMsgID))}),
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

func clearCodexPlanModeForSession(a *App, sessionKey string) (bool, error) {
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
	defaultMode, err := resolveDefaultCodexCollaborationModeForSession(a, sess)
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

func firstNonEmptyWorkspace(a *App, workspaceID string) *config.Workspace {
	if a == nil {
		return nil
	}
	return config.FindWorkspace(a.cfg, workspaceID)
}

func codexPlanModeExitFreshPrompt(planMarkdown string) string {
	planMarkdown = strings.TrimSpace(planMarkdown)
	intro := "A previous agent produced the plan below to accomplish the user's task. Implement the plan in a fresh context. Treat the plan as the source of user intent, re-read files as needed, and carry the work through implementation and verification."
	if planMarkdown == "" {
		return intro
	}
	return intro + "\n\n" + planMarkdown
}

func codexPlanModeExitPlanMarkdownFromPending(pending *state.PendingRequest) string {
	if pending == nil {
		return ""
	}
	return strings.TrimSpace(codexPlanModeExitPayloadFromPending(pending).PlanMarkdown)
}
