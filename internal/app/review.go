package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appreview "feidex/internal/app/review"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const (
	submissionKindReview = "review"
	pendingKindReview    = "review_form"

	reviewFormModeBase   = "base"
	reviewFormModeCommit = "commit"
	reviewFormModeCustom = "custom"

	gitRecordSep = "\x1e"
	gitFieldSep  = "\x1f"
)

type reviewPendingPayload struct {
	Mode         string `json:"mode"`
	Branch       string `json:"branch,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	CommitTitle  string `json:"commit_title,omitempty"`
	Instructions string `json:"instructions,omitempty"`
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

func (s conversationWorkflowService) commandReview(msg *feishu.InboundMessage, args []string) error {
	if msg == nil {
		return nil
	}
	if len(args) == 0 {
		return s.app.startInlineReviewFromMessage(msg, appreview.TargetSpec{Type: appreview.TargetUncommitted})
	}
	switch strings.TrimSpace(args[0]) {
	case "uncommitted", "uncommittedChanges":
		if len(args) != 1 {
			return fmt.Errorf("usage: /review | /review uncommitted | /review base [branch] | /review commit [rev] | /review custom [instructions]")
		}
		return s.app.startInlineReviewFromMessage(msg, appreview.TargetSpec{Type: appreview.TargetUncommitted})
	case "base":
		switch len(args) {
		case 1:
			return newReviewFormService(s.app).beginReviewForm(msg, reviewFormModeBase)
		case 2:
			return s.app.startInlineReviewFromMessage(msg, appreview.TargetSpec{
				Type:   appreview.TargetBaseBranch,
				Branch: strings.TrimSpace(args[1]),
			})
		default:
			return fmt.Errorf("usage: /review base [branch]")
		}
	case "commit":
		switch len(args) {
		case 1:
			return newReviewFormService(s.app).beginReviewForm(msg, reviewFormModeCommit)
		case 2:
			return s.app.startInlineReviewFromMessage(msg, appreview.TargetSpec{
				Type:      appreview.TargetCommit,
				CommitSHA: strings.TrimSpace(args[1]),
			})
		default:
			return fmt.Errorf("usage: /review commit [rev]")
		}
	case "custom":
		if len(args) == 1 {
			return newReviewFormService(s.app).beginReviewForm(msg, reviewFormModeCustom)
		}
		return s.app.startInlineReviewFromMessage(msg, appreview.TargetSpec{
			Type:         appreview.TargetCustom,
			Instructions: strings.TrimSpace(strings.Join(args[1:], " ")),
		})
	default:
		return fmt.Errorf("usage: /review | /review uncommitted | /review base [branch] | /review commit [rev] | /review custom [instructions]")
	}
}

func (a *App) startInlineReviewFromMessage(msg *feishu.InboundMessage, target appreview.TargetSpec) error {
	confirmation, err := a.startInlineReview(msg, target)
	if err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, confirmation, replyInThreadEnabled(a, msg.ChatType))
}

func (a *App) startInlineReview(msg *feishu.InboundMessage, target appreview.TargetSpec) (string, error) {
	if msg == nil {
		return "", fmt.Errorf("nil message")
	}
	sessionKey, sess, ws := newWorkspaceConfigService(a).currentWorkspaceForMessage(msg)
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
	resolved, err := newReviewGitService(a).resolveReviewTarget(ws.Cwd, target)
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
	return appreview.ConfirmationText(resolved), nil
}

func (a *App) enqueueReviewSubmission(msg *feishu.InboundMessage, sessionKey string, ws *config.Workspace, threadID string, target appreview.TargetSpec) error {
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
		InputText:            appreview.SubmissionInputText(target),
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
		if err := startNextSubmission(a, sessionKey); err != nil {
			return err
		}
		if !willWaitInQueue {
			return nil
		}
	}
	newPendingQueueService(a).markSubmissionQueuedReactions(sub)
	a.sendSubmissionQueuedNotice(context.Background(), sub)
	return nil
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
	client, err := requireCodexClient(a)
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

func reviewTargetFromSubmission(sub *state.Submission) appreview.TargetSpec {
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

func reviewTargetParams(target appreview.TargetSpec) map[string]any {
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
