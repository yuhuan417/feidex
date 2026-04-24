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
	if a.isStaleInboundMessage(msg) {
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
	newRuntimeStateService(a).beginFrontendMessageTraffic()
	defer newRuntimeStateService(a).finishFrontendMessageTraffic()
	markHandled := func() {
		if a.deduper != nil {
			a.deduper.MarkDone(msg.MessageID)
		}
		releaseClaim = false
	}
	if err := r.processMessage(msg); err != nil {
		_ = a.replyError(msg, err)
		return
	}
	markHandled()
}

func (r *feishuEventRouter) processMessage(msg *feishu.InboundMessage) error {
	a := r.app
	if msg == nil {
		return nil
	}
	sessionKey := a.makeSessionKey(msg)
	logText := truncate(msg.Text, 160)
	if a.shouldRedactInboundText(sessionKey, msg.UserID) {
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
	a.flushPendingFrontendCardNotifications(msg)
	if len(msg.MergeForwardMessageIDs) > 0 {
		a.startMergeForwardPrefetch(msg)
		return nil
	}
	if !a.hasConfiguredBackend() {
		if strings.TrimSpace(msg.Text) == "" && len(msg.Attachments) == 0 {
			return nil
		}
		return a.replyBackendSelectionCard(msg, "")
	}
	if !msg.ExpandedMergeForward {
		if pending := a.pendingTextRequest(sessionKey, msg.UserID); pending != nil && !strings.HasPrefix(strings.TrimSpace(msg.Text), "/") && len(msg.Attachments) == 0 {
			if err := newPendingInputService(a).handlePendingTextResponse(msg, pending); err != nil {
				return err
			}
			return nil
		}
	}
	if !msg.ExpandedMergeForward && strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		if isLocalCommandForBackend(a.configuredBackend(), strings.TrimSpace(msg.Text)) {
			if err := newCommandService(a).handleCommand(msg, strings.TrimSpace(msg.Text)); err != nil {
				return err
			}
			return nil
		}
	}
	if err := a.backendMaintenanceBlocksInboundMessage(); err != nil {
		return err
	}
	replyLink := a.replyRootTurnLink(msg)
	targetSessionKey := a.makeSessionKey(msg)
	if replyLink != nil {
		targetSessionKey = a.sessionKeyForInboundMessage(msg, replyLink)
	}
	if newPendingQueueService(a).shouldStageInboundImages(msg) {
		if err := newPendingQueueService(a).stageInboundImagesForSession(msg, a.makeSessionKey(msg)); err != nil {
			return err
		}
		return nil
	}
	if strings.TrimSpace(msg.Text) == "" && len(msg.Attachments) == 0 {
		return nil
	}
	if replyLink != nil {
		if steered, err := a.trySteerInboundReply(msg, replyLink); err == nil && steered {
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
	if err := a.enqueueSubmissionWithSessionKey(msg, targetSessionKey, replyLink != nil); err != nil {
		return err
	}
	return nil
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
	if err := newCommandService(a).handleCommand(msg, click.Command); err != nil {
		_ = a.feishu.SendText(context.Background(), click.UserID, "命令执行失败: "+err.Error())
	}
}
