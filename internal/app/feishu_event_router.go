package app

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/feishu"
)

// feishuEventRouter groups Feishu inbound event entrypoints.
type feishuEventRouter struct {
	app *App
}

func newFeishuEventRouter(app *App) *feishuEventRouter {
	return &feishuEventRouter{app: app}
}

func (r *feishuEventRouter) handleMessage(msg *feishu.InboundMessage) {
	a := r.app
	if msg == nil {
		return
	}
	if isStaleInboundMessage(a.started, msg) {
		slog.Debug("feishu stale message ignored", "message_id", msg.MessageID, "created_at", msg.CreatedAt)
		return
	}
	if a.deduper != nil && !a.deduper.Claim(msg.MessageID) {
		slog.Debug("feishu duplicate message ignored by inbound deduper", "message_id", msg.MessageID)
		return
	}
	releaseClaim := true
	defer func() {
		if releaseClaim && a.deduper != nil {
			a.deduper.Release(msg.MessageID)
		}
	}()
	rss := newRuntimeStateService(a)
	rss.beginFrontendMessageTraffic()
	defer rss.finishFrontendMessageTraffic()
	markHandled := func() {
		if a.deduper != nil {
			a.deduper.MarkDone(msg.MessageID)
		}
		releaseClaim = false
	}
	if err := r.processMessage(msg); err != nil {
		_ = replyError(a, msg, err)
		return
	}
	markHandled()
}

func (r *feishuEventRouter) processMessage(msg *feishu.InboundMessage) error {
	a := r.app
	if msg == nil {
		return nil
	}
	if msg.ChatType == "group" {
		scheduleGroupAnnouncementStatusRefresh(a, msg.ChatID, "group_message")
		if _, err := ensureGroupPrimaryInitialized(context.Background(), a, msg.ChatType, msg.ChatID); err != nil {
			slog.Warn("group primary auto init failed during message processing",
				"frontend_id", strings.TrimSpace(a.FrontendID()),
				"message_id", msg.MessageID,
				"chat_id", msg.ChatID,
				"error", err,
			)
		}
		if handled, err := syncGroupPrimaryAssignment(a, msg); handled || err != nil {
			if err != nil {
				return err
			}
			slog.Debug("feishu group primary assignment synced by non-target bot",
				"frontend_id", strings.TrimSpace(a.FrontendID()),
				"message_id", msg.MessageID,
				"chat_id", msg.ChatID,
				"owner_bot_open_id", groupPrimaryOwnerOpenID(a, msg.ChatType, msg.ChatID),
			)
			return nil
		}
	}
	if msg.ChatType == "group" && !shouldAcceptGroupMessage(
		a,
		msg.ChatID,
		groupPolicyRootMessageID(msg),
		msg.ParentMessageID,
		msg.MentionedSelf,
		msg.MentionedAny || len(msg.MentionedOpenIDs) > 0,
	) {
		slog.Debug("feishu group message ignored by app group policy",
			"frontend_id", strings.TrimSpace(a.FrontendID()),
			"message_id", msg.MessageID,
			"chat_id", msg.ChatID,
			"root_message_id", msg.RootMessageID,
			"policy_root_message_id", groupPolicyRootMessageID(msg),
			"parent_message_id", msg.ParentMessageID,
			"mentioned_self", msg.MentionedSelf,
			"mention_count", len(msg.MentionedOpenIDs),
			"mentioned_any", msg.MentionedAny,
		)
		return nil
	}
	if emptyGroupAtPrimaryCommand(msg) {
		slog.Info("feishu empty self mention promoted to primary command",
			"frontend_id", strings.TrimSpace(a.FrontendID()),
			"message_id", msg.MessageID,
			"chat_id", msg.ChatID,
			"chat_type", msg.ChatType,
			"user_id", msg.UserID,
			"root_message_id", msg.RootMessageID,
			"mentioned_self", msg.MentionedSelf,
			"mention_count", len(msg.MentionedOpenIDs),
			"raw_text", msg.Text,
		)
		msg = cloneInboundMessageWithText(msg, "/primary on")
	}
	sessionKey := makeSessionKey(a, msg)
	logText := truncate(msg.Text, 160)
	if a.ServerRequestService().ShouldRedactInboundText(sessionKey, msg.UserID) {
		logText = "[redacted pending input]"
	}
	slog.Debug("feishu inbound",
		"message_id", msg.MessageID,
		"chat_id", msg.ChatID,
		"chat_type", msg.ChatType,
		"user_id", msg.UserID,
		"root_message_id", msg.RootMessageID,
		"text", logText,
		"attachment_count", len(msg.Attachments),
		"merge_forward_count", len(msg.MergeForwardMessageIDs),
	)
	flushPendingFrontendCardNotifications(a, msg)
	if len(msg.MergeForwardMessageIDs) > 0 {
		startMergeForwardPrefetch(a, msg)
		return nil
	}
	if handled, err := newBindingService(a).gatePendingGroupMessage(msg); handled || err != nil {
		return err
	}
	if !hasConfiguredBackend(a) {
		if strings.TrimSpace(msg.Text) == "" && len(msg.Attachments) == 0 {
			return nil
		}
		return newBackendSelectionService(a).replyBackendSelectionCard(msg, "")
	}
	if !msg.ExpandedMergeForward && !strings.HasPrefix(strings.TrimSpace(msg.Text), "/") && len(msg.Attachments) == 0 {
		if pending := a.ServerRequestService().PendingTextRequest(sessionKey, msg.UserID); pending != nil {
			if err := a.ServerRequestService().HandlePendingTextResponse(msg, pending); err != nil {
				return err
			}
			return nil
		}
		if pending := rootPendingTextRequest(a, sessionKey, msg.UserID); pending != nil {
			if err := handleRootPendingTextResponse(a, msg, pending); err != nil {
				return err
			}
			return nil
		}
	}
	if !msg.ExpandedMergeForward && strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		if isLocalCommandForMessage(configuredBackend(a), msg, strings.TrimSpace(msg.Text)) {
			if err := handleCommand(a, msg, strings.TrimSpace(msg.Text)); err != nil {
				return err
			}
			return nil
		}
	}
	if reason := newRuntimeStateService(a).backendSwitchBlockedReasonForTraffic(); reason != "" {
		return newUIWarningError(reason)
	}
	if runtime := backendRuntime(a); runtime != nil {
		if err := runtime.maintenanceBlocksCommand(a, ""); err != nil {
			return err
		}
	}
	rcs := newReplyContinuationService(a)
	replyLink := rcs.replyRootTurnLink(msg)
	targetSessionKey := makeSessionKey(a, msg)
	if replyLink != nil {
		targetSessionKey = rcs.sessionKeyForInboundMessage(msg, replyLink)
	}
	pqs := newPendingQueueService(a)
	if pqs.shouldStageInboundImages(msg) {
		if err := pqs.stageInboundImagesForSession(msg, makeSessionKey(a, msg)); err != nil {
			return err
		}
		return nil
	}
	if strings.TrimSpace(msg.Text) == "" && len(msg.Attachments) == 0 {
		return nil
	}
	if replyLink != nil {
		if steered, err := rcs.trySteerInboundReply(msg, replyLink); err == nil && steered {
			return nil
		} else if err != nil {
			slog.Warn("reply steer failed; falling back to queue",
				"message_id", msg.MessageID,
				"parent_message_id", msg.ParentMessageID,
				"thread_id", firstNonEmpty(replyLink.ThreadID, ""),
				"turn_id", firstNonEmpty(replyLink.TurnID, ""),
				"error", err,
			)
		}
	}
	if err := enqueueSubmissionWithSessionKey(a, msg, targetSessionKey, replyLink != nil); err != nil {
		return err
	}
	return nil
}

func emptyGroupAtPrimaryCommand(msg *feishu.InboundMessage) bool {
	if msg == nil {
		return false
	}
	if strings.TrimSpace(msg.ChatType) != "group" {
		return false
	}
	if !msg.MentionedSelf {
		return false
	}
	if len(msg.MentionedOpenIDs) == 0 {
		return false
	}
	if strings.TrimSpace(msg.Text) != "" {
		return false
	}
	if len(msg.Attachments) != 0 || len(msg.MergeForwardMessageIDs) != 0 {
		return false
	}
	return true
}

func cloneInboundMessageWithText(msg *feishu.InboundMessage, text string) *feishu.InboundMessage {
	if msg == nil {
		return nil
	}
	cloned := *msg
	cloned.Text = text
	return &cloned
}

func groupPolicyRootMessageID(msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	rootMessageID := strings.TrimSpace(msg.RootMessageID)
	if rootMessageID == "" {
		return ""
	}
	if strings.TrimSpace(msg.ParentMessageID) == "" && rootMessageID == strings.TrimSpace(msg.MessageID) {
		return ""
	}
	return rootMessageID
}

func (r *feishuEventRouter) handleRecall(recall *feishu.MessageRecall) {
	a := r.app
	if recall == nil || strings.TrimSpace(recall.MessageID) == "" {
		return
	}
	if discarded := newPendingQueueService(a).discardPendingInputByMessageID(recall.MessageID); discarded {
		slog.Debug("feishu recall discarded pending input", "message_id", recall.MessageID, "chat_id", recall.ChatID)
	}
}

func (r *feishuEventRouter) handleReaction(reaction *feishu.MessageReaction) {
	a := r.app
	if reaction == nil || strings.TrimSpace(reaction.MessageID) == "" {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(reaction.EmojiType), discardReactionEmoji) {
		return
	}
	if discarded := newPendingQueueService(a).discardPendingInputByMessageID(reaction.MessageID); discarded {
		slog.Debug("feishu reaction discarded pending input",
			"message_id", reaction.MessageID,
			"chat_id", reaction.ChatID,
			"user_id", reaction.UserID,
			"emoji_type", reaction.EmojiType,
		)
	}
}

func (r *feishuEventRouter) handleBotMenu(click *feishu.BotMenuClick) {
	a := r.app
	if click == nil {
		return
	}
	msg := &feishu.InboundMessage{
		UserID:   click.UserID,
		ChatID:   click.UserID,
		ChatType: "p2p",
		Text:     click.Command,
	}
	if err := handleCommand(a, msg, click.Command); err != nil {
		_ = a.feishu.SendText(context.Background(), click.UserID, "命令执行失败: "+err.Error())
	}
}
