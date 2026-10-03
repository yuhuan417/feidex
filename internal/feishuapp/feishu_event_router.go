package feishuapp

import (
	"feidex/internal/application"
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
	if a.runtimeOwner.InboundDeduper != nil && !a.runtimeOwner.InboundDeduper.Claim(msg.MessageID) {
		slog.Debug("feishu duplicate message ignored by inbound deduper", "message_id", msg.MessageID)
		return
	}
	releaseClaim := true
	defer func() {
		if releaseClaim && a.runtimeOwner.InboundDeduper != nil {
			a.runtimeOwner.InboundDeduper.Release(msg.MessageID)
		}
	}()
	a.runtimeOwner.
		BeginMessageTraffic()
	defer a.runtimeOwner.
		EndMessageTraffic()
	markHandled := func() {
		if a.runtimeOwner.InboundDeduper != nil {
			a.runtimeOwner.InboundDeduper.MarkDone(msg.MessageID)
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
	return r.app.bindings.Inbound.ProcessMessage(msg)
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
	discarded := a.bindings.Inbound.DiscardMessage(recall.MessageID)
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
	discarded := a.bindings.Inbound.DiscardMessage(reaction.MessageID)
	if discarded {
		slog.Debug("feishu reaction discarded pending input",
			"message_id", reaction.MessageID,
			"chat_id", reaction.ChatID,
			"user_id", reaction.UserID,
			"emoji_type", reaction.EmojiType,
		)
	}
}
