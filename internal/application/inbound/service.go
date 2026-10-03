// Package inbound owns message routing and submission admission.
package inbound

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/application"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"
	"feidex/internal/textutil"
)

type Routing interface {
	NormalizeGroupMessage(*application.InboundMessage) bool
	EnsurePrimary(context.Context, string, string) error
	SyncPrimary(*application.InboundMessage) (bool, error)
	DropsGroupMessage(*application.InboundMessage) bool
}

type Requests interface {
	ShouldRedactInboundText(string, string) bool
	PendingTextRequest(string, string) *interaction.PendingRequest
	HandlePendingTextResponse(*application.InboundMessage, *interaction.PendingRequest) error
}

type RootInputs interface {
	PendingTextRequest(string, string) *interaction.PendingRequest
	HandlePendingTextResponse(*application.InboundMessage, *interaction.PendingRequest) error
}

type Continuation interface {
	ReplyRootTurnLink(*application.InboundMessage) *conversation.MessageLink
	SessionKeyForInboundMessage(*application.InboundMessage, *conversation.MessageLink) string
	TrySteerInboundReply(*application.InboundMessage, *conversation.MessageLink) (bool, error)
}

type PendingInputs interface {
	ShouldStageInboundImages(*application.InboundMessage) bool
	StageInboundImages(*application.InboundMessage, string) error
	DiscardPendingInputByMessageID(string) bool
}

type Bindings interface {
	GatePendingGroupMessage(*application.InboundMessage) (bool, error)
	DiscardPendingMessage(string) bool
}

type Commands interface {
	IsLocalCommand(*application.InboundMessage, string) bool
	HandleCommand(*application.InboundMessage, string) error
}

type BackendAdmission interface {
	Configured() bool
	ReplySelection(*application.InboundMessage) error
	BlockedReason() string
	CheckMaintenance() error
}

type SubmissionQueue interface {
	EnqueueSubmission(*application.InboundMessage, string, bool) error
}

type Dependencies struct {
	FrontendID         string
	Context            func() context.Context
	SessionKey         func(*application.InboundMessage) string
	Routing            Routing
	Requests           Requests
	RootInputs         RootInputs
	Continuation       Continuation
	Pending            PendingInputs
	Bindings           Bindings
	Commands           Commands
	Backend            BackendAdmission
	Queue              SubmissionQueue
	RefreshGroup       func(string, string)
	FlushNotifications func(*application.InboundMessage)
	PrefetchForward    func(*application.InboundMessage)
}

type Service struct{ Deps Dependencies }

func (s Service) ProcessMessage(msg *application.InboundMessage) error {
	if msg == nil {
		return nil
	}
	d := s.Deps
	if msg.ChatType == "group" {
		if !d.Routing.NormalizeGroupMessage(msg) {
			return nil
		}
		d.RefreshGroup(msg.ChatID, "group_message")
		if err := d.Routing.EnsurePrimary(d.Context(), msg.ChatType, msg.ChatID); err != nil {
			slog.Warn("group primary auto init failed during message processing", "frontend_id", d.FrontendID, "message_id", msg.MessageID, "chat_id", msg.ChatID, "error", err)
		}
		if handled, err := d.Routing.SyncPrimary(msg); handled || err != nil {
			return err
		}
	}
	if d.Routing.DropsGroupMessage(msg) {
		return nil
	}
	key := d.SessionKey(msg)
	text := textutil.Truncate(msg.Text, 160)
	if d.Requests.ShouldRedactInboundText(key, msg.UserID) {
		text = "[redacted pending input]"
	}
	slog.Debug("feishu inbound", "message_id", msg.MessageID, "chat_id", msg.ChatID, "chat_type", msg.ChatType, "user_id", msg.UserID, "root_message_id", msg.RootMessageID, "text", text, "attachment_count", len(msg.Attachments), "merge_forward_count", len(msg.MergeForwardMessageIDs))
	d.FlushNotifications(msg)
	if len(msg.MergeForwardMessageIDs) > 0 {
		d.PrefetchForward(msg)
		return nil
	}
	if handled, err := d.Bindings.GatePendingGroupMessage(msg); handled || err != nil {
		return err
	}
	if !d.Backend.Configured() {
		if strings.TrimSpace(msg.Text) == "" && len(msg.Attachments) == 0 {
			return nil
		}
		return d.Backend.ReplySelection(msg)
	}
	trimmed := strings.TrimSpace(msg.Text)
	serverPending := d.Requests.PendingTextRequest(key, msg.UserID)
	rootPending := d.RootInputs.PendingTextRequest(key, msg.UserID)
	route := application.ClassifyMessageRoute(application.MessageRouteInput{
		ExpandedMergeForward: msg.ExpandedMergeForward, TextEmpty: trimmed == "", HasAttachments: len(msg.Attachments) > 0,
		StartsCommand: strings.HasPrefix(trimmed, "/"), LocalCommand: d.Commands.IsLocalCommand(msg, trimmed),
		PendingServerText: serverPending != nil, PendingRootText: rootPending != nil,
	})
	switch route {
	case application.MessageRoutePendingServerText:
		return d.Requests.HandlePendingTextResponse(msg, serverPending)
	case application.MessageRoutePendingRootText:
		return d.RootInputs.HandlePendingTextResponse(msg, rootPending)
	case application.MessageRouteLocalCommand:
		return d.Commands.HandleCommand(msg, trimmed)
	}
	if reason := d.Backend.BlockedReason(); reason != "" {
		return conversation.NewWarning(reason)
	}
	if err := d.Backend.CheckMaintenance(); err != nil {
		return err
	}
	link := d.Continuation.ReplyRootTurnLink(msg)
	targetKey := key
	if link != nil {
		targetKey = d.Continuation.SessionKeyForInboundMessage(msg, link)
	}
	route = application.ClassifyMessageRoute(application.MessageRouteInput{
		ExpandedMergeForward: msg.ExpandedMergeForward, TextEmpty: trimmed == "", HasAttachments: len(msg.Attachments) > 0,
		StartsCommand: strings.HasPrefix(trimmed, "/"), StageImages: d.Pending.ShouldStageInboundImages(msg),
	})
	if route == application.MessageRouteStageImages {
		return d.Pending.StageInboundImages(msg, key)
	}
	if route == application.MessageRouteNoop {
		return nil
	}
	if link != nil {
		if steered, err := d.Continuation.TrySteerInboundReply(msg, link); err == nil && steered {
			return nil
		} else if err != nil {
			slog.Warn("reply steer failed; falling back to queue", "message_id", msg.MessageID, "parent_message_id", msg.ParentMessageID, "thread_id", link.ThreadID, "turn_id", link.TurnID, "error", err)
		}
	}
	if err := d.Queue.EnqueueSubmission(msg, targetKey, link != nil); err != nil {
		return err
	}
	return nil
}

func (s Service) DiscardMessage(messageID string) bool {
	if strings.TrimSpace(messageID) == "" {
		return false
	}
	pending := s.Deps.Pending.DiscardPendingInputByMessageID(messageID)
	return s.Deps.Bindings.DiscardPendingMessage(messageID) || pending
}
