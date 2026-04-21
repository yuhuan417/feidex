package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) replyRootTurnLink(msg *feishu.InboundMessage) *state.MessageLink {
	if a == nil || a.store == nil || msg == nil {
		return nil
	}
	if strings.TrimSpace(msg.ParentMessageID) == "" {
		return nil
	}
	root := strings.TrimSpace(msg.RootMessageID)
	if root == "" || root == strings.TrimSpace(msg.MessageID) {
		return nil
	}
	link := a.appState().messageLink(root)
	if !a.messageLinkMatchesCurrentBackend(link) {
		return nil
	}
	return link
}

func (a *App) messageLinkMatchesCurrentBackend(link *state.MessageLink) bool {
	if a == nil || link == nil {
		return false
	}
	currentBackend := a.configuredBackend()
	linkBackend := normalizeRuntimeBackend(link.Backend)
	switch {
	case currentBackend == "":
		return linkBackend == ""
	case linkBackend != "":
		return linkBackend == currentBackend
	}
	if strings.TrimSpace(link.SessionKey) == "" || strings.TrimSpace(link.ThreadID) == "" {
		return false
	}
	sess := a.appState().session(link.SessionKey)
	if sess == nil {
		return false
	}
	return strings.TrimSpace(sess.ActiveThreadID) == strings.TrimSpace(link.ThreadID)
}

func (a *App) sessionKeyForInboundMessage(msg *feishu.InboundMessage, link *state.MessageLink) string {
	if link != nil && strings.TrimSpace(link.SessionKey) != "" {
		return strings.TrimSpace(link.SessionKey)
	}
	return a.makeSessionKey(msg)
}

func (a *App) pendingInputSessionKey(msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	prefix := "feishu:"
	if strings.TrimSpace(a.frontendID) != "" {
		prefix += "frontend:" + strings.TrimSpace(a.frontendID) + ":"
	}
	if strings.TrimSpace(msg.ChatType) == "group" {
		return prefix + "group:" + strings.TrimSpace(msg.ChatID) + ":pending:" + strings.TrimSpace(msg.UserID)
	}
	return prefix + "p2p:" + strings.TrimSpace(msg.ChatID) + ":pending:" + strings.TrimSpace(msg.UserID)
}

func (a *App) collectPendingStagedImages(targetSessionKey, bucketSessionKey string) []state.SessionStagedImage {
	appState := a.appState()
	images := []state.SessionStagedImage{}
	seen := map[string]struct{}{}
	for _, key := range []string{strings.TrimSpace(bucketSessionKey), strings.TrimSpace(targetSessionKey)} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sess := appState.session(key)
		if sess == nil {
			continue
		}
		images = append(images, sess.StagedImages...)
	}
	sort.SliceStable(images, func(i, j int) bool {
		if images[i].CreatedAt == images[j].CreatedAt {
			return images[i].SourceMessageID < images[j].SourceMessageID
		}
		return images[i].CreatedAt < images[j].CreatedAt
	})
	return images
}

func (a *App) clearPendingStagedImages(targetSessionKey, bucketSessionKey string) error {
	appState := a.appState()
	seen := map[string]struct{}{}
	for _, key := range []string{strings.TrimSpace(bucketSessionKey), strings.TrimSpace(targetSessionKey)} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sess := appState.session(key)
		if sess == nil || len(sess.StagedImages) == 0 {
			continue
		}
		sess.StagedImages = nil
		if !sessionHasInFlightSubmission(sess) && len(sess.Queue) == 0 {
			sess.Status = "idle"
		}
		if err := appState.saveSession(sess); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) trySteerInboundReply(msg *feishu.InboundMessage, link *state.MessageLink) (bool, error) {
	if a == nil || msg == nil || link == nil {
		return false, nil
	}
	appState := a.appState()
	threadID := strings.TrimSpace(link.ThreadID)
	turnID := strings.TrimSpace(link.TurnID)
	if threadID == "" || turnID == "" {
		return false, nil
	}
	sessionKey := a.sessionKeyForInboundMessage(msg, link)
	sess := appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   a.defaultWorkspaceID(),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        "idle",
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = a.defaultWorkspaceID()
	}
	if a.isClaudeBackend() {
		return a.tryClaudeReplyContinuation(msg, link, sessionKey, sess)
	}
	bucketSessionKey := a.pendingInputSessionKey(msg)
	inboundAttachments, err := a.resolveInboundAttachments(msg, sess.WorkspaceID, sessionKey)
	if err != nil {
		return false, err
	}
	stagedImages := a.collectPendingStagedImages(sessionKey, bucketSessionKey)
	inputSub := &state.Submission{
		InputText:            msg.Text,
		Attachments:          append(stagedImageAttachments(stagedImages), inboundAttachments...),
		WorkspaceID:          sess.WorkspaceID,
		SessionKey:           sessionKey,
		TriggerMessageID:     msg.MessageID,
		SourceRootMessageIDs: uniqueStrings(append([]string{firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))}, stagedImageRootMessageIDs(stagedImages)...)),
	}
	inputs := buildTurnInputs(inputSub)
	if len(inputs) == 0 {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.codex.Call(ctx, "turn/steer", map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input":          inputs,
	}, nil); err != nil {
		return false, err
	}
	sess.WorkspaceID = firstNonEmpty(sess.WorkspaceID, a.defaultWorkspaceID())
	if err := appState.saveSession(sess); err != nil {
		return false, err
	}
	if err := a.clearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) tryClaudeReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	if a == nil || msg == nil || link == nil || sess == nil {
		return false, nil
	}
	if !sessionHasInFlightSubmission(sess) {
		return false, nil
	}
	if strings.TrimSpace(sess.ActiveThreadID) == "" {
		return false, nil
	}
	sub, err := a.buildClaudeContinuationSubmissionFromMessage(msg, sessionKey, sess, true)
	if err != nil {
		return false, err
	}
	if sub == nil {
		return false, nil
	}
	if err := a.startClaudeContinuationSubmission(sessionKey, sub, false); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) continueClaudeSessionWithText(sessionKey, text string) error {
	if a == nil {
		return fmt.Errorf("app not initialized")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	appState := a.appState()
	sess := appState.session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveTurnID) == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), a.defaultWorkspaceID())
	sub := &state.Submission{
		SessionKey:  strings.TrimSpace(sessionKey),
		WorkspaceID: workspaceID,
		UserID:      strings.TrimSpace(sess.OwnerUserID),
		ChatID:      strings.TrimSpace(sess.ChatID),
		InputText:   text,
		Status:      "queued",
	}
	if rootMessageID := strings.TrimSpace(sess.RootMessageID); rootMessageID != "" {
		sub.SourceRootMessageIDs = []string{rootMessageID}
	}
	id, err := appState.createSubmission(sub)
	if err != nil {
		return err
	}
	sub.ID = id
	return a.startClaudeContinuationSubmission(sessionKey, sub, false)
}

func (a *App) buildClaudeContinuationSubmissionFromMessage(msg *feishu.InboundMessage, sessionKey string, sess *state.Session, bindOnlyCurrentRoot bool) (*state.Submission, error) {
	if a == nil || msg == nil || sess == nil {
		return nil, nil
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), a.defaultWorkspaceID())
	bucketSessionKey := a.pendingInputSessionKey(msg)
	inboundAttachments, err := a.resolveInboundAttachments(msg, workspaceID, sessionKey)
	if err != nil {
		return nil, err
	}
	stagedImages := a.collectPendingStagedImages(sessionKey, bucketSessionKey)
	sourceMessageIDs := uniqueStrings(append([]string{msg.MessageID}, stagedImageSourceMessageIDs(stagedImages)...))
	currentRootMessageID := firstNonEmpty(strings.TrimSpace(msg.RootMessageID), strings.TrimSpace(msg.MessageID))
	sourceRootMessageIDs := []string{currentRootMessageID}
	if !bindOnlyCurrentRoot {
		sourceRootMessageIDs = uniqueStrings(append(sourceRootMessageIDs, stagedImageRootMessageIDs(stagedImages)...))
	}
	sub := &state.Submission{
		SessionKey:           sessionKey,
		WorkspaceID:          workspaceID,
		UserID:               msg.UserID,
		ChatID:               msg.ChatID,
		TriggerMessageID:     msg.MessageID,
		SourceMessageIDs:     sourceMessageIDs,
		SourceRootMessageIDs: sourceRootMessageIDs,
		InputText:            msg.Text,
		Attachments:          append(stagedImageAttachments(stagedImages), inboundAttachments...),
		Status:               "queued",
	}
	if strings.TrimSpace(sub.InputText) == "" && len(sub.Attachments) == 0 {
		return nil, nil
	}
	id, err := a.appState().createSubmission(sub)
	if err != nil {
		return nil, err
	}
	sub.ID = id
	if len(stagedImages) > 0 {
		if err := a.clearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
			return nil, err
		}
	}
	return sub, nil
}

func (a *App) startClaudeContinuationSubmission(sessionKey string, sub *state.Submission, notifyFailure bool) error {
	if a == nil || sub == nil {
		return nil
	}
	appState := a.appState()
	sess := appState.session(sessionKey)
	if sess == nil {
		return fmt.Errorf("session %q missing", sessionKey)
	}
	ws := config.FindWorkspace(a.cfg, sub.WorkspaceID)
	if ws == nil {
		return fmt.Errorf("workspace %q not found", sub.WorkspaceID)
	}
	return newSubmissionWorkflow(a).startNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
}

func (a *App) recordSubmissionSourceLinks(sub *state.Submission) {
	if a == nil || a.store == nil || sub == nil {
		return
	}
	sourceMessageIDs := sourceMessageIDsForSubmission(sub)
	if len(sub.SourceRootMessageIDs) > 1 {
		for _, messageID := range sourceMessageIDs {
			a.recordTurnMessageLink(messageID, sub.SessionKey, sub.ThreadID, sub.TurnID)
		}
	} else if strings.TrimSpace(sub.TriggerMessageID) != "" {
		a.recordTurnMessageLink(sub.TriggerMessageID, sub.SessionKey, sub.ThreadID, sub.TurnID)
	} else {
		for _, messageID := range sourceMessageIDs {
			a.recordTurnMessageLink(messageID, sub.SessionKey, sub.ThreadID, sub.TurnID)
		}
	}
	for _, rootID := range sub.SourceRootMessageIDs {
		a.recordRootTurnBinding(rootID, sub.SessionKey, sub.ThreadID, sub.TurnID)
	}
}

func (a *App) recordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID string) {
	if a == nil || a.store == nil || strings.TrimSpace(rootMessageID) == "" {
		return
	}
	_ = a.appState().saveMessageLink(&state.MessageLink{
		MessageID:  strings.TrimSpace(rootMessageID),
		SessionKey: strings.TrimSpace(sessionKey),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	})
}

func (a *App) recordTurnMessageLink(messageID, sessionKey, threadID, turnID string) {
	if a == nil || a.store == nil || strings.TrimSpace(messageID) == "" {
		return
	}
	_ = a.appState().saveMessageLink(&state.MessageLink{
		MessageID:  strings.TrimSpace(messageID),
		SessionKey: strings.TrimSpace(sessionKey),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	})
}
