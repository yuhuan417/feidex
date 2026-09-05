package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (s bindingService) gatePendingGroupMessage(msg *feishu.InboundMessage) (bool, error) {
	if s.app == nil || msg == nil || !isGroupMessage(msg) {
		return false, nil
	}
	if isBindingOnboardingCommand(msg.Text) {
		return false, nil
	}
	binding := agentBindingForChat(s.app, msg.ChatType, msg.ChatID)
	if binding == nil {
		if !msg.MentionedSelf && !isGroupPrimary(s.app, msg.ChatType, msg.ChatID) {
			return false, nil
		}
		var err error
		binding, err = s.ensureBindingForMessage(msg)
		if err != nil {
			return false, err
		}
	}
	if bindingReadyForInput(binding) {
		return false, nil
	}
	pending := pendingBindingMessageFromInbound(s.app, msg)
	_, err := s.updateBinding(binding, func(current *state.AgentBinding) {
		if pending == nil {
			return
		}
		current.PendingMessages = append(current.PendingMessages, pending)
		current.PendingMessage = pending
	})
	if err != nil {
		return false, err
	}
	card := newWorkspaceRenderServiceInner(s.app).RenderWorkspaceMenuCard(makeSessionKey(s.app, msg))
	_, err = s.app.feishu.ReplyCard(context.Background(), msg.MessageID, card, replyInThreadEnabled(s.app, msg.ChatType))
	return true, err
}

func bindingReadyForInput(binding *state.AgentBinding) bool {
	if binding == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(binding.Status), state.AgentBindingStatusActive.String()) && strings.TrimSpace(binding.WorkspaceID) != ""
}

func isBindingOnboardingCommand(raw string) bool {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(fields[0])) {
	case "/workspace", "/primary", "/menu", "/model", "/effort", "/fast", "/help", "/backend":
		return true
	default:
		return false
	}
}

func pendingBindingMessageFromInbound(a *App, msg *feishu.InboundMessage) *state.AgentBindingPendingMessage {
	if msg == nil {
		return nil
	}
	attachments := make([]state.AgentBindingPendingAttachment, 0, len(msg.Attachments))
	for _, attachment := range msg.Attachments {
		attachments = append(attachments, state.AgentBindingPendingAttachment{
			Kind:            attachment.Kind,
			ResourceKey:     attachment.ResourceKey,
			SourceMessageID: attachment.SourceMessageID,
		})
	}
	return &state.AgentBindingPendingMessage{
		SessionKey:             makeSessionKey(a, msg),
		MessageID:              strings.TrimSpace(msg.MessageID),
		ChatID:                 strings.TrimSpace(msg.ChatID),
		ChatType:               strings.TrimSpace(msg.ChatType),
		UserID:                 strings.TrimSpace(msg.UserID),
		UserName:               strings.TrimSpace(msg.UserName),
		ChatName:               strings.TrimSpace(msg.ChatName),
		Text:                   msg.Text,
		RootMessageID:          strings.TrimSpace(msg.RootMessageID),
		ParentMessageID:        strings.TrimSpace(msg.ParentMessageID),
		ThreadID:               strings.TrimSpace(msg.ThreadID),
		Attachments:            attachments,
		MergeForwardMessageIDs: append([]string(nil), msg.MergeForwardMessageIDs...),
		ExpandedMergeForward:   msg.ExpandedMergeForward,
		MentionedOpenIDs:       append([]string(nil), msg.MentionedOpenIDs...),
		MentionedAny:           msg.MentionedAny,
		MentionedSelf:          msg.MentionedSelf,
		CreatedAt:              msg.CreatedAt,
		StoredAt:               time.Now().Unix(),
	}
}

func inboundFromPendingBindingMessage(pending *state.AgentBindingPendingMessage) *feishu.InboundMessage {
	if pending == nil {
		return nil
	}
	attachments := make([]feishu.Attachment, 0, len(pending.Attachments))
	for _, attachment := range pending.Attachments {
		attachments = append(attachments, feishu.Attachment{
			Kind:            attachment.Kind,
			ResourceKey:     attachment.ResourceKey,
			SourceMessageID: attachment.SourceMessageID,
		})
	}
	return &feishu.InboundMessage{
		SessionKey:             strings.TrimSpace(pending.SessionKey),
		MessageID:              strings.TrimSpace(pending.MessageID),
		ChatID:                 strings.TrimSpace(pending.ChatID),
		ChatType:               strings.TrimSpace(pending.ChatType),
		UserID:                 strings.TrimSpace(pending.UserID),
		UserName:               strings.TrimSpace(pending.UserName),
		ChatName:               strings.TrimSpace(pending.ChatName),
		Text:                   pending.Text,
		RootMessageID:          strings.TrimSpace(pending.RootMessageID),
		ParentMessageID:        strings.TrimSpace(pending.ParentMessageID),
		ThreadID:               strings.TrimSpace(pending.ThreadID),
		Attachments:            attachments,
		MergeForwardMessageIDs: append([]string(nil), pending.MergeForwardMessageIDs...),
		ExpandedMergeForward:   pending.ExpandedMergeForward,
		MentionedOpenIDs:       append([]string(nil), pending.MentionedOpenIDs...),
		MentionedAny:           pending.MentionedAny,
		MentionedSelf:          pending.MentionedSelf,
		CreatedAt:              pending.CreatedAt,
	}
}

func (s bindingService) replayPendingBindingMessage(binding *state.AgentBinding) error {
	if s.app == nil || binding == nil || !bindingReadyForInput(binding) {
		return nil
	}
	for {
		current := s.app.State().AgentBinding(binding.ID)
		if current == nil || len(current.PendingMessages) == 0 {
			return nil
		}
		pending := current.PendingMessages[0]
		msg := inboundFromPendingBindingMessage(pending)
		if msg == nil {
			_, err := s.updateBinding(current, func(updated *state.AgentBinding) {
				updated.PendingMessage = nil
				if len(updated.PendingMessages) > 0 {
					updated.PendingMessages = updated.PendingMessages[1:]
				}
			})
			if err != nil {
				return err
			}
			continue
		}
		sessionKey := strings.TrimSpace(pending.SessionKey)
		if sessionKey == "" {
			sessionKey = makeSessionKey(s.app, msg)
		}
		if err := enqueueSubmissionWithSessionKey(s.app, msg, sessionKey, false); err != nil {
			return err
		}
		_, err := s.updateBinding(current, func(updated *state.AgentBinding) {
			updated.PendingMessage = nil
			if len(updated.PendingMessages) > 0 && strings.TrimSpace(updated.PendingMessages[0].MessageID) == strings.TrimSpace(pending.MessageID) {
				updated.PendingMessages = updated.PendingMessages[1:]
			}
		})
		if err != nil {
			return err
		}
	}
}

func (s bindingService) replayPendingBindingMessageAsync(binding *state.AgentBinding) {
	if binding == nil || len(binding.PendingMessages) == 0 {
		return
	}
	messageID := strings.TrimSpace(binding.PendingMessages[0].MessageID)
	runAsync(s.app, func() {
		if err := s.replayPendingBindingMessage(binding); err != nil {
			slog.Warn("binding pending message replay failed", "binding_id", binding.ID, "message_id", messageID, "error", err)
			if messageID != "" {
				_ = s.app.feishu.ReplyText(context.Background(), messageID, fmt.Sprintf("绑定成功，但处理原消息失败: %v", err), replyInThreadEnabled(s.app, binding.ChatType))
			}
		}
	})
}

func discardPendingBindingMessageByID(a *App, messageID string) bool {
	if a == nil || strings.TrimSpace(messageID) == "" || a.State() == nil {
		return false
	}
	discarded := false
	for _, binding := range a.State().AgentBindings() {
		if binding == nil || len(binding.PendingMessages) == 0 {
			continue
		}
		filtered := make([]*state.AgentBindingPendingMessage, 0, len(binding.PendingMessages))
		matched := false
		for _, pending := range binding.PendingMessages {
			if pending != nil && strings.TrimSpace(pending.MessageID) == strings.TrimSpace(messageID) {
				discarded = true
				matched = true
				continue
			}
			filtered = append(filtered, pending)
		}
		if !matched {
			continue
		}
		binding.PendingMessages = filtered
		if len(filtered) > 0 {
			binding.PendingMessage = filtered[0]
		} else {
			binding.PendingMessage = nil
		}
		_ = a.State().SaveAgentBinding(binding)
	}
	return discarded
}
