package continuation

import (
	"context"
	"feidex/internal/application"
	appsubmission "feidex/internal/application/submission"
	"feidex/internal/domain/conversation"
	domainsubmission "feidex/internal/domain/submission"
	"feidex/internal/textutil"
	"fmt"
	"strings"
	"time"
)

func firstNonEmpty(values ...string) string { return textutil.FirstNonEmpty(values...) }
func valueOrEmpty(fn func() string) string {
	if fn == nil {
		return ""
	}
	return strings.TrimSpace(fn())
}
func dependencyContext(fn func() context.Context) context.Context {
	if fn != nil {
		return fn()
	}
	return context.Background()
}

type CodexReplyContinuationDeps struct {
	Context                    func() context.Context
	Steer                      func(context.Context, string, string, *domainsubmission.Submission) error
	ResolveInboundAttachments  func(msg *application.InboundMessage, workspaceID, sessionKey string) ([]domainsubmission.SubmissionAttachment, error)
	PendingInputSessionKey     func(msg *application.InboundMessage) string
	CollectPendingStagedImages func(sessionKey, bucketSessionKey string) []conversation.SessionStagedImage
	ClearPendingStagedImages   func(sessionKey, bucketSessionKey string) error
	SaveSession                func(*conversation.Session) error
	DefaultWorkspaceID         func() string
}

func TryCodexReplyContinuation(deps CodexReplyContinuationDeps, msg *application.InboundMessage, link *conversation.MessageLink, sessionKey string, sess *conversation.Session) (bool, error) {
	if msg == nil || link == nil {
		return false, nil
	}
	threadID := strings.TrimSpace(link.ThreadID)
	turnID := strings.TrimSpace(link.TurnID)
	if threadID == "" || turnID == "" {
		return false, nil
	}
	if sess == nil {
		sess = &conversation.Session{
			Key:           sessionKey,
			WorkspaceID:   valueOrEmpty(deps.DefaultWorkspaceID),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        conversation.SessionStatusIdle.String(),
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = valueOrEmpty(deps.DefaultWorkspaceID)
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), valueOrEmpty(deps.DefaultWorkspaceID))
	var inboundAttachments []domainsubmission.SubmissionAttachment
	var err error
	if deps.ResolveInboundAttachments != nil {
		inboundAttachments, err = deps.ResolveInboundAttachments(msg, workspaceID, sessionKey)
		if err != nil {
			return false, err
		}
	}
	bucketSessionKey := ""
	if deps.PendingInputSessionKey != nil {
		bucketSessionKey = deps.PendingInputSessionKey(msg)
	}
	var stagedImages []conversation.SessionStagedImage
	if deps.CollectPendingStagedImages != nil {
		stagedImages = deps.CollectPendingStagedImages(sessionKey, bucketSessionKey)
	}
	inputSub := &domainsubmission.Submission{
		InputText:            msg.Text,
		Attachments:          append(appsubmission.StagedImageAttachments(stagedImages), inboundAttachments...),
		WorkspaceID:          workspaceID,
		SessionKey:           sessionKey,
		TriggerMessageID:     msg.MessageID,
		SourceRootMessageIDs: appsubmission.UniqueStrings(append([]string{firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))}, appsubmission.StagedImageRootMessageIDs(stagedImages)...)),
	}
	if strings.TrimSpace(inputSub.InputText) == "" && len(inputSub.Attachments) == 0 {
		return false, nil
	}
	if deps.Steer == nil {
		return false, fmt.Errorf("codex client not initialized")
	}
	ctx, cancel := context.WithTimeout(dependencyContext(deps.Context), 20*time.Second)
	defer cancel()
	if err := deps.Steer(ctx, threadID, turnID, inputSub); err != nil {
		return false, err
	}
	sess.WorkspaceID = firstNonEmpty(sess.WorkspaceID, valueOrEmpty(deps.DefaultWorkspaceID))
	if deps.SaveSession != nil {
		if err := deps.SaveSession(sess); err != nil {
			return false, err
		}
	}
	if deps.ClearPendingStagedImages != nil {
		if err := deps.ClearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
			return false, err
		}
	}
	return true, nil
}
