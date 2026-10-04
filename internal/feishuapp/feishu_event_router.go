package feishuapp

import (
	"feidex/internal/application"
	"feidex/internal/application/inbound"
	"feidex/internal/application/routing"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/feishu"
)

// feishuEventRouter groups Feishu inbound event entrypoints.
type feishuEventRouter struct {
	started   time.Time
	inbound   inboundMessageService
	deduper   inboundMessageDeduper
	traffic   inboundMessageTraffic
	replyFail func(*feishu.InboundMessage, error)
}

type inboundMessageService interface {
	ProcessMessage(*application.InboundMessage) error
	DiscardMessage(string) bool
}

type inboundMessageDeduper interface {
	Claim(string) bool
	Release(string)
	MarkDone(string)
}

type inboundMessageTraffic interface {
	BeginMessageTraffic()
	EndMessageTraffic()
}

func newFeishuEventRouter(started time.Time, service inboundMessageService, deduper inboundMessageDeduper, traffic inboundMessageTraffic, replyFail func(*feishu.InboundMessage, error)) *feishuEventRouter {
	return &feishuEventRouter{started: started, inbound: service, deduper: deduper, traffic: traffic, replyFail: replyFail}
}

func (r *feishuEventRouter) handleMessage(msg *feishu.InboundMessage) {
	if msg == nil {
		return
	}
	if isStaleInboundMessage(r.started, msg) {
		slog.Debug("feishu stale message ignored", "message_id", msg.MessageID, "created_at", msg.CreatedAt)
		return
	}
	if r.deduper != nil && !r.deduper.Claim(msg.MessageID) {
		slog.Debug("feishu duplicate message ignored by inbound deduper", "message_id", msg.MessageID)
		return
	}
	releaseClaim := true
	defer func() {
		if releaseClaim && r.deduper != nil {
			r.deduper.Release(msg.MessageID)
		}
	}()
	if r.traffic != nil {
		r.traffic.BeginMessageTraffic()
		defer r.traffic.EndMessageTraffic()
	}
	markHandled := func() {
		if r.deduper != nil {
			r.deduper.MarkDone(msg.MessageID)
		}
		releaseClaim = false
	}
	if err := r.processMessage(msg); err != nil {
		if r.replyFail != nil {
			r.replyFail(msg, err)
		}
		return
	}
	markHandled()
}

func (r *feishuEventRouter) processMessage(msg *feishu.InboundMessage) error {
	return r.inbound.ProcessMessage(msg)
}

// routerDropsGroupMessage is the router-level group gate, the second of two.
//
// The adapter already decided delivery through shouldDeliverGroupMessageToApp;
// this one narrows further. The two must agree, and @所有人 is where they can
// drift apart: that path answers to nobody in particular and is delivered to
// every bot, so this gate must not apply to it. Without the MentionAll check a
// non-primary bot passes the adapter and is dropped here.
func routerDropsGroupMessage(groupMessages routing.GroupMessages, msg *feishu.InboundMessage) bool {
	if msg == nil || msg.ChatType != "group" {
		return false
	}
	if msg.MentionAll {
		return false
	}
	return !shouldAcceptGroupMessage(
		groupMessages,
		msg.ChatID,
		application.GroupPolicyRootMessageID(msg.MessageID, msg.RootMessageID, msg.ParentMessageID),
		msg.ParentMessageID,
		msg.MentionedSelf,
		msg.MentionedAny || len(msg.MentionedOpenIDs) > 0,
	)
}

func (r *feishuEventRouter) handleRecall(recall *feishu.MessageRecall) {
	if recall == nil || strings.TrimSpace(recall.MessageID) == "" {
		return
	}
	discarded := r.inbound.DiscardMessage(recall.MessageID)
	if discarded {
		slog.Debug("feishu recall discarded pending input", "message_id", recall.MessageID, "chat_id", recall.ChatID)
	}
}

func (r *feishuEventRouter) handleReaction(reaction *feishu.MessageReaction) {
	if reaction == nil || strings.TrimSpace(reaction.MessageID) == "" {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(reaction.EmojiType), discardReactionEmoji) {
		return
	}
	discarded := r.inbound.DiscardMessage(reaction.MessageID)
	if discarded {
		slog.Debug("feishu reaction discarded pending input",
			"message_id", reaction.MessageID,
			"chat_id", reaction.ChatID,
			"user_id", reaction.UserID,
			"emoji_type", reaction.EmojiType,
		)
	}
}

var _ inboundMessageService = (*inbound.Service)(nil)
