package app

import (
	"context"
	"sort"
	"strings"
	"time"

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
	return a.store.GetMessageLink(root)
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
	if strings.TrimSpace(msg.ChatType) == "group" {
		return "feishu:group:" + strings.TrimSpace(msg.ChatID) + ":pending:" + strings.TrimSpace(msg.UserID)
	}
	return "feishu:p2p:" + strings.TrimSpace(msg.ChatID) + ":pending:" + strings.TrimSpace(msg.UserID)
}

func (a *App) collectPendingStagedImages(targetSessionKey, bucketSessionKey string) []state.SessionStagedImage {
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
		sess := a.store.GetSession(key)
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
	seen := map[string]struct{}{}
	for _, key := range []string{strings.TrimSpace(bucketSessionKey), strings.TrimSpace(targetSessionKey)} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sess := a.store.GetSession(key)
		if sess == nil || len(sess.StagedImages) == 0 {
			continue
		}
		sess.StagedImages = nil
		if !sessionHasInFlightSubmission(sess) && len(sess.Queue) == 0 {
			sess.Status = "idle"
		}
		if err := a.store.UpsertSession(sess); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) trySteerInboundReply(msg *feishu.InboundMessage, link *state.MessageLink) (bool, error) {
	if a == nil || msg == nil || link == nil {
		return false, nil
	}
	threadID := strings.TrimSpace(link.ThreadID)
	turnID := strings.TrimSpace(link.TurnID)
	if threadID == "" || turnID == "" {
		return false, nil
	}
	sessionKey := a.sessionKeyForInboundMessage(msg, link)
	sess := a.store.GetSession(sessionKey)
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
	if err := a.store.UpsertSession(sess); err != nil {
		return false, err
	}
	if err := a.clearPendingStagedImages(sessionKey, bucketSessionKey); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) recordInboundSubmissionSourceLink(messageID, sessionKey, submissionID string) {
	if a == nil || a.store == nil || strings.TrimSpace(messageID) == "" {
		return
	}
	_ = a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:    strings.TrimSpace(messageID),
		Kind:         "submission_source",
		SessionKey:   strings.TrimSpace(sessionKey),
		SubmissionID: strings.TrimSpace(submissionID),
	})
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
	_ = a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:  strings.TrimSpace(rootMessageID),
		Kind:       "root_turn",
		SessionKey: strings.TrimSpace(sessionKey),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	})
}

func (a *App) recordTurnMessageLink(messageID, sessionKey, threadID, turnID string) {
	if a == nil || a.store == nil || strings.TrimSpace(messageID) == "" {
		return
	}
	_ = a.store.UpsertMessageLink(&state.MessageLink{
		MessageID:  strings.TrimSpace(messageID),
		Kind:       "turn_source",
		SessionKey: strings.TrimSpace(sessionKey),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	})
}
