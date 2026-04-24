package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
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
		return s.app.startInlineReviewFromMessage(msg, reviewTargetSpec{Type: reviewTargetUncommitted})
	}
	switch strings.TrimSpace(args[0]) {
	case "uncommitted", "uncommittedChanges":
		if len(args) != 1 {
			return fmt.Errorf("usage: /review | /review uncommitted | /review base [branch] | /review commit [rev] | /review custom [instructions]")
		}
		return s.app.startInlineReviewFromMessage(msg, reviewTargetSpec{Type: reviewTargetUncommitted})
	case "base":
		switch len(args) {
		case 1:
			return s.app.beginReviewForm(msg, reviewFormModeBase)
		case 2:
			return s.app.startInlineReviewFromMessage(msg, reviewTargetSpec{
				Type:   reviewTargetBaseBranch,
				Branch: strings.TrimSpace(args[1]),
			})
		default:
			return fmt.Errorf("usage: /review base [branch]")
		}
	case "commit":
		switch len(args) {
		case 1:
			return s.app.beginReviewForm(msg, reviewFormModeCommit)
		case 2:
			return s.app.startInlineReviewFromMessage(msg, reviewTargetSpec{
				Type:      reviewTargetCommit,
				CommitSHA: strings.TrimSpace(args[1]),
			})
		default:
			return fmt.Errorf("usage: /review commit [rev]")
		}
	case "custom":
		if len(args) == 1 {
			return s.app.beginReviewForm(msg, reviewFormModeCustom)
		}
		return s.app.startInlineReviewFromMessage(msg, reviewTargetSpec{
			Type:         reviewTargetCustom,
			Instructions: strings.TrimSpace(strings.Join(args[1:], " ")),
		})
	default:
		return fmt.Errorf("usage: /review | /review uncommitted | /review base [branch] | /review commit [rev] | /review custom [instructions]")
	}
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
	client, err := a.requireCodexClient()
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
