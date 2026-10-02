package app

import (
	"feidex/internal/application"
	"feidex/internal/textutil"
	"log/slog"
	"strings"

	domainrouting "feidex/internal/domain/routing"
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
		// The adapter's cached bot identity is only a transport hint. Use the
		// mention open_id from the event as the authoritative routing signal so
		// two frontends in one process cannot disagree about the target bot.
		msg.MentionedSelf = messageMentionsCurrentBot(a, msg.MentionedOpenIDs, msg.MentionedSelf)
		if _, ok := groupPrimaryAssignmentFromMessage(msg); ok && domainrouting.ParseEmptyBotMention(msg.Text) {
			msg.Text = "/primary on"
		}
		if isGroupPrimaryControlMessage(msg) {
			if _, ok := groupPrimaryAssignmentFromMessage(msg); !ok {
				return nil
			}
		}
	}
	if msg.ChatType == "group" {
		scheduleGroupAnnouncementStatusRefresh(a, msg.ChatID, "group_message")
		if _, err := ensureGroupPrimaryInitialized(a.Context(), a, msg.ChatType, msg.ChatID); err != nil {
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
				"primary_enabled", isGroupPrimary(a, msg.ChatType, msg.ChatID),
			)
			return nil
		}
	}
	if routerDropsGroupMessage(a, msg) {
		slog.Debug("feishu group message ignored by app group policy",
			"frontend_id", strings.TrimSpace(a.FrontendID()),
			"message_id", msg.MessageID,
			"chat_id", msg.ChatID,
			"root_message_id", msg.RootMessageID,
			"policy_root_message_id", application.GroupPolicyRootMessageID(msg.MessageID, msg.RootMessageID, msg.ParentMessageID),
			"parent_message_id", msg.ParentMessageID,
			"mentioned_self", msg.MentionedSelf,
			"mention_count", len(msg.MentionedOpenIDs),
			"mentioned_any", msg.MentionedAny,
		)
		return nil
	}
	sessionKey := makeSessionKey(a, msg)
	logText := textutil.Truncate(msg.Text, 160)
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
	trimmedText := strings.TrimSpace(msg.Text)
	startsCommand := strings.HasPrefix(trimmedText, "/")
	serverPending := a.ServerRequestService().PendingTextRequest(sessionKey, msg.UserID)
	rootPending := rootPendingTextRequest(a, sessionKey, msg.UserID)
	initialRoute := application.ClassifyMessageRoute(application.MessageRouteInput{
		ExpandedMergeForward: msg.ExpandedMergeForward,
		TextEmpty:            trimmedText == "",
		HasAttachments:       len(msg.Attachments) > 0,
		StartsCommand:        startsCommand,
		LocalCommand:         isLocalCommandForMessage(configuredBackend(a), msg, trimmedText),
		PendingServerText:    serverPending != nil,
		PendingRootText:      rootPending != nil,
	})
	switch initialRoute {
	case application.MessageRoutePendingServerText:
		return a.ServerRequestService().HandlePendingTextResponse(msg, serverPending)
	case application.MessageRoutePendingRootText:
		return handleRootPendingTextResponse(a, msg, rootPending)
	case application.MessageRouteLocalCommand:
		return handleCommand(a, msg, trimmedText)
	}
	if reason := newRuntimeStateService(a).backendSwitchBlockedReasonForTraffic(); reason != "" {
		return newUIWarningError(reason)
	}
	if runtime := backendRuntime(a); runtime != nil {
		if err := runtime.maintenanceBlocksCommand(backendRuntimeContextForApp(a), ""); err != nil {
			return err
		}
	}
	rcs := newReplyContinuationService(a)
	replyLink := rcs.ReplyRootTurnLink(msg)
	targetSessionKey := makeSessionKey(a, msg)
	if replyLink != nil {
		targetSessionKey = rcs.SessionKeyForInboundMessage(msg, replyLink)
	}
	pqs := newPendingQueueService(a)
	route := application.ClassifyMessageRoute(application.MessageRouteInput{
		ExpandedMergeForward: msg.ExpandedMergeForward,
		TextEmpty:            trimmedText == "",
		HasAttachments:       len(msg.Attachments) > 0,
		StartsCommand:        startsCommand,
		StageImages:          pqs.shouldStageInboundImages(msg),
	})
	if route == application.MessageRouteStageImages {
		if err := pqs.stageInboundImagesForSession(msg, makeSessionKey(a, msg)); err != nil {
			return err
		}
		return nil
	}
	if route == application.MessageRouteNoop {
		return nil
	}
	if replyLink != nil {
		if steered, err := rcs.TrySteerInboundReply(msg, replyLink); err == nil && steered {
			return nil
		} else if err != nil {
			slog.Warn("reply steer failed; falling back to queue",
				"message_id", msg.MessageID,
				"parent_message_id", msg.ParentMessageID,
				"thread_id", textutil.FirstNonEmpty(replyLink.ThreadID, ""),
				"turn_id", textutil.FirstNonEmpty(replyLink.TurnID, ""),
				"error", err,
			)
		}
	}
	if err := enqueueSubmissionWithSessionKey(a, msg, targetSessionKey, replyLink != nil); err != nil {
		return err
	}
	return nil
}

// routerDropsGroupMessage is the router-level group gate, the second of two.
//
// The adapter already decided delivery through shouldDeliverGroupMessageToApp;
// this one narrows further. The two must agree, and @所有人 is where they can
// drift apart: that path answers to nobody in particular and is delivered to
// every bot, so this gate must not apply to it. Without the MentionAll check a
// non-primary bot passes the adapter and is dropped here.
func routerDropsGroupMessage(a *App, msg *feishu.InboundMessage) bool {
	if msg == nil || msg.ChatType != "group" {
		return false
	}
	if msg.MentionAll {
		return false
	}
	return !shouldAcceptGroupMessage(
		a,
		msg.ChatID,
		application.GroupPolicyRootMessageID(msg.MessageID, msg.RootMessageID, msg.ParentMessageID),
		msg.ParentMessageID,
		msg.MentionedSelf,
		msg.MentionedAny || len(msg.MentionedOpenIDs) > 0,
	)
}

func (r *feishuEventRouter) handleRecall(recall *feishu.MessageRecall) {
	a := r.app
	if recall == nil || strings.TrimSpace(recall.MessageID) == "" {
		return
	}
	discarded := newPendingQueueService(a).discardPendingInputByMessageID(recall.MessageID)
	if discardPendingBindingMessageByID(a, recall.MessageID) {
		discarded = true
	}
	if discarded {
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
	discarded := newPendingQueueService(a).discardPendingInputByMessageID(reaction.MessageID)
	if discardPendingBindingMessageByID(a, reaction.MessageID) {
		discarded = true
	}
	if discarded {
		slog.Debug("feishu reaction discarded pending input",
			"message_id", reaction.MessageID,
			"chat_id", reaction.ChatID,
			"user_id", reaction.UserID,
			"emoji_type", reaction.EmojiType,
		)
	}
}
