package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/feishu"
)

const mergeForwardPrefetchTimeout = 30 * time.Second

func (a *App) startMergeForwardPrefetch(msg *feishu.InboundMessage) {
	if a == nil || a.feishu == nil || msg == nil || len(msg.MergeForwardMessageIDs) == 0 {
		return
	}
	ids := append([]string(nil), msg.MergeForwardMessageIDs...)
	prepared := cloneInboundMessage(msg)
	a.markMessagesQueuedReactions([]string{msg.MessageID})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), mergeForwardPrefetchTimeout)
		defer cancel()

		slog.Debug("merge_forward prefetch start",
			"message_id", msg.MessageID,
			"count", len(ids),
		)
		text, attachments, err := a.feishu.ResolveMergeForward(ctx, msg.MessageID, ids)
		if err != nil {
			a.clearMessageProcessingReactions([]string{msg.MessageID})
			_ = a.replyError(msg, fmt.Errorf("合并转发预取失败: %w", err))
			return
		}

		prepared.Text = text
		prepared.Attachments = attachments
		prepared.MergeForwardMessageIDs = nil
		prepared.ExpandedMergeForward = true
		if strings.TrimSpace(prepared.Text) == "" && len(prepared.Attachments) == 0 {
			a.clearMessageProcessingReactions([]string{msg.MessageID})
			_ = a.replyError(msg, fmt.Errorf("合并转发预取失败: empty expanded content"))
			return
		}

		if err := newFeishuEventRouter(a).processMessage(prepared); err != nil {
			a.clearMessageProcessingReactions([]string{msg.MessageID})
			_ = a.replyError(msg, err)
			return
		}
	}()
}

func cloneInboundMessage(msg *feishu.InboundMessage) *feishu.InboundMessage {
	if msg == nil {
		return nil
	}
	cp := *msg
	cp.Attachments = append([]feishu.Attachment(nil), msg.Attachments...)
	cp.MergeForwardMessageIDs = append([]string(nil), msg.MergeForwardMessageIDs...)
	return &cp
}
