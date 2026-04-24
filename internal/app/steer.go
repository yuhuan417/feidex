package app

import (
	"fmt"
	"sort"
	"strings"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (s replyContinuationService) replyRootTurnLink(msg *feishu.InboundMessage) *state.MessageLink {
	if s.app == nil || s.app.store == nil || msg == nil {
		return nil
	}
	if strings.TrimSpace(msg.ParentMessageID) == "" {
		return nil
	}
	root := strings.TrimSpace(msg.RootMessageID)
	if root == "" || root == strings.TrimSpace(msg.MessageID) {
		return nil
	}
	link := appState(s.app).messageLink(root)
	if !newReplyContinuationService(s.app).messageLinkMatchesCurrentBackend(link) {
		return nil
	}
	return link
}

func (s replyContinuationService) messageLinkMatchesCurrentBackend(link *state.MessageLink) bool {
	if s.app == nil || link == nil {
		return false
	}
	currentBackend := configuredBackend(s.app)
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
	sess := appState(s.app).session(link.SessionKey)
	if sess == nil {
		return false
	}
	return strings.TrimSpace(sess.ActiveThreadID) == strings.TrimSpace(link.ThreadID)
}

func (s replyContinuationService) sessionKeyForInboundMessage(msg *feishu.InboundMessage, link *state.MessageLink) string {
	if link != nil && strings.TrimSpace(link.SessionKey) != "" {
		return strings.TrimSpace(link.SessionKey)
	}
	return makeSessionKey(s.app, msg)
}

func (s replyContinuationService) pendingInputSessionKey(msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	prefix := "feishu:"
	if strings.TrimSpace(s.app.frontendID) != "" {
		prefix += "frontend:" + strings.TrimSpace(s.app.frontendID) + ":"
	}
	if strings.TrimSpace(msg.ChatType) == "group" {
		return prefix + "group:" + strings.TrimSpace(msg.ChatID) + ":pending:" + strings.TrimSpace(msg.UserID)
	}
	return prefix + "p2p:" + strings.TrimSpace(msg.ChatID) + ":pending:" + strings.TrimSpace(msg.UserID)
}

func (s replyContinuationService) collectPendingStagedImages(targetSessionKey, bucketSessionKey string) []state.SessionStagedImage {
	appState := appState(s.app)
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

func (s replyContinuationService) clearPendingStagedImages(targetSessionKey, bucketSessionKey string) error {
	appState := appState(s.app)
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

func (s replyContinuationService) trySteerInboundReply(msg *feishu.InboundMessage, link *state.MessageLink) (bool, error) {
	if s.app == nil || msg == nil || link == nil {
		return false, nil
	}
	appState := appState(s.app)
	threadID := strings.TrimSpace(link.ThreadID)
	turnID := strings.TrimSpace(link.TurnID)
	if threadID == "" || turnID == "" {
		return false, nil
	}
	sessionKey := newReplyContinuationService(s.app).sessionKeyForInboundMessage(msg, link)
	sess := appState.session(sessionKey)
	if sess == nil {
		sess = &state.Session{
			Key:           sessionKey,
			WorkspaceID:   defaultWorkspaceID(s.app),
			OwnerUserID:   msg.UserID,
			ChatID:        msg.ChatID,
			ChatType:      msg.ChatType,
			RootMessageID: msg.RootMessageID,
			Status:        "idle",
		}
	}
	if strings.TrimSpace(sess.WorkspaceID) == "" {
		sess.WorkspaceID = defaultWorkspaceID(s.app)
	}
	return conversationBackend(s.app).tryReplyContinuation(msg, link, sessionKey, sess)
}

func (s replyContinuationService) tryClaudeReplyContinuation(msg *feishu.InboundMessage, link *state.MessageLink, sessionKey string, sess *state.Session) (bool, error) {
	if s.app == nil || msg == nil || link == nil || sess == nil {
		return false, nil
	}
	if !sessionHasInFlightSubmission(sess) {
		return false, nil
	}
	if strings.TrimSpace(sess.ActiveThreadID) == "" {
		return false, nil
	}
	sub, err := newReplyContinuationService(s.app).buildClaudeContinuationSubmissionFromMessage(msg, sessionKey, sess, true)
	if err != nil {
		return false, err
	}
	if sub == nil {
		return false, nil
	}
	if err := newReplyContinuationService(s.app).startClaudeContinuationSubmission(sessionKey, sub, false); err != nil {
		return false, err
	}
	return true, nil
}

func (s replyContinuationService) continueClaudeSessionWithText(sessionKey, text string) error {
	if s.app == nil {
		return fmt.Errorf("app not initialized")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	appState := appState(s.app)
	sess := appState.session(sessionKey)
	if sess == nil || strings.TrimSpace(sess.ActiveThreadID) == "" || strings.TrimSpace(sess.ActiveTurnID) == "" {
		return fmt.Errorf("当前没有可补充的任务")
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), defaultWorkspaceID(s.app))
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
	return newReplyContinuationService(s.app).startClaudeContinuationSubmission(sessionKey, sub, false)
}

func (s replyContinuationService) buildClaudeContinuationSubmissionFromMessage(msg *feishu.InboundMessage, sessionKey string, sess *state.Session, bindOnlyCurrentRoot bool) (*state.Submission, error) {
	if s.app == nil || msg == nil || sess == nil {
		return nil, nil
	}
	workspaceID := firstNonEmpty(strings.TrimSpace(sess.WorkspaceID), defaultWorkspaceID(s.app))
	bucketSessionKey := newReplyContinuationService(s.app).pendingInputSessionKey(msg)
	inboundAttachments, err := resolveInboundAttachments(s.app, msg, workspaceID, sessionKey)
	if err != nil {
		return nil, err
	}
	stagedImages := newReplyContinuationService(s.app).collectPendingStagedImages(sessionKey, bucketSessionKey)
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
	id, err := appState(s.app).createSubmission(sub)
	if err != nil {
		return nil, err
	}
	sub.ID = id
	if len(stagedImages) > 0 {
		if err := newReplyContinuationService(s.app).clearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
			return nil, err
		}
	}
	return sub, nil
}

func (s replyContinuationService) startClaudeContinuationSubmission(sessionKey string, sub *state.Submission, notifyFailure bool) error {
	if s.app == nil || sub == nil {
		return nil
	}
	appState := appState(s.app)
	sess := appState.session(sessionKey)
	if sess == nil {
		return fmt.Errorf("session %q missing", sessionKey)
	}
	ws := config.FindWorkspace(s.app.cfg, sub.WorkspaceID)
	if ws == nil {
		return fmt.Errorf("workspace %q not found", sub.WorkspaceID)
	}
	return newLifecycleCoordinator(s.app).startNextClaudeSubmissionWithFailureNotice(sessionKey, sess, sub, ws, notifyFailure)
}

func (s replyContinuationService) recordSubmissionSourceLinks(sub *state.Submission) {
	if s.app == nil || s.app.store == nil || sub == nil {
		return
	}
	sourceMessageIDs := sourceMessageIDsForSubmission(sub)
	if len(sub.SourceRootMessageIDs) > 1 {
		for _, messageID := range sourceMessageIDs {
			newReplyContinuationService(s.app).recordTurnMessageLink(messageID, sub.SessionKey, sub.ThreadID, sub.TurnID)
		}
	} else if strings.TrimSpace(sub.TriggerMessageID) != "" {
		newReplyContinuationService(s.app).recordTurnMessageLink(sub.TriggerMessageID, sub.SessionKey, sub.ThreadID, sub.TurnID)
	} else {
		for _, messageID := range sourceMessageIDs {
			newReplyContinuationService(s.app).recordTurnMessageLink(messageID, sub.SessionKey, sub.ThreadID, sub.TurnID)
		}
	}
	for _, rootID := range sub.SourceRootMessageIDs {
		newReplyContinuationService(s.app).recordRootTurnBinding(rootID, sub.SessionKey, sub.ThreadID, sub.TurnID)
	}
}

func (s replyContinuationService) recordRootTurnBinding(rootMessageID, sessionKey, threadID, turnID string) {
	if s.app == nil || s.app.store == nil || strings.TrimSpace(rootMessageID) == "" {
		return
	}
	_ = appState(s.app).saveMessageLink(&state.MessageLink{
		MessageID:  strings.TrimSpace(rootMessageID),
		SessionKey: strings.TrimSpace(sessionKey),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	})
}

func (s replyContinuationService) recordTurnMessageLink(messageID, sessionKey, threadID, turnID string) {
	if s.app == nil || s.app.store == nil || strings.TrimSpace(messageID) == "" {
		return
	}
	_ = appState(s.app).saveMessageLink(&state.MessageLink{
		MessageID:  strings.TrimSpace(messageID),
		SessionKey: strings.TrimSpace(sessionKey),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	})
}
