package routing

import (
	"feidex/internal/application"
	"feidex/internal/domain/identity"
	domain "feidex/internal/domain/routing"
	"fmt"
	"strings"
)

type PendingRepository interface {
	ConfigurationRepository
	AgentBindings() []*domain.AgentBinding
}
type PendingService struct {
	Configuration ConfigurationService
	Repository    PendingRepository
}
type PendingGate struct {
	Handled bool
	Effects []application.Effect
}

func BindingReady(binding *domain.AgentBinding) bool {
	return binding != nil && strings.EqualFold(strings.TrimSpace(binding.Status), "active") && strings.TrimSpace(binding.WorkspaceID) != ""
}
func OnboardingCommand(raw string) bool {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "/workspace", "/primary", "/menu", "/model", "/effort", "/fast", "/help", "/backend":
		return true
	}
	return false
}
func (s PendingService) Gate(msg *application.InboundMessage, sessionKey string, primary bool, storedAt int64) (PendingGate, error) {
	if s.Repository == nil {
		return PendingGate{}, fmt.Errorf("pending repository is nil")
	}
	if msg == nil || !strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") || OnboardingCommand(msg.Text) {
		return PendingGate{}, nil
	}
	var binding *domain.AgentBinding
	for _, candidate := range s.Repository.AgentBindingsForChat(msg.ChatType, msg.ChatID) {
		if candidate != nil {
			binding = candidate
			break
		}
	}
	if binding == nil {
		if !msg.MentionedSelf && !primary {
			return PendingGate{}, nil
		}
		var err error
		binding, err = s.Configuration.EnsureBinding(msg.ChatType, msg.ChatID)
		if err != nil {
			return PendingGate{}, err
		}
	}
	if BindingReady(binding) {
		return PendingGate{}, nil
	}
	pending := PendingFromMessage(msg, sessionKey, storedAt)
	result, err := s.Configuration.UpdateBinding(binding, func(current *domain.AgentBinding) {
		current.PendingMessages = append(current.PendingMessages, pending)
		current.PendingMessage = pending
	})
	return PendingGate{Handled: err == nil, Effects: result.Effects}, err
}

type PendingReplay struct {
	Pending *domain.AgentBindingPendingMessage
	Effects []application.Effect
	Exists  bool
}

func (s PendingService) Next(bindingID string) PendingReplay {
	if s.Repository == nil {
		return PendingReplay{}
	}
	binding := s.Repository.AgentBinding(bindingID)
	if !BindingReady(binding) || len(binding.PendingMessages) == 0 {
		return PendingReplay{}
	}
	pending := binding.PendingMessages[0]
	next := PendingReplay{Pending: pending, Exists: true}
	if msg := MessageFromPending(pending); msg != nil {
		sessionKey := strings.TrimSpace(pending.SessionKey)
		if sessionKey == "" {
			sessionKey = identity.CanonicalSessionKey(string(s.Configuration.Frontend), "feishu:chat:"+msg.ChatID)
		}
		next.Effects = []application.Effect{application.EnqueueInput{Frontend: s.Configuration.Frontend, SessionKey: sessionKey, Message: *msg}}
	}
	return next
}
func (s PendingService) Acknowledge(bindingID string, pending *domain.AgentBindingPendingMessage) ([]application.Effect, error) {
	if s.Repository == nil {
		return nil, fmt.Errorf("pending repository is nil")
	}
	binding := s.Repository.AgentBinding(bindingID)
	if binding == nil {
		return nil, fmt.Errorf("binding %q not found", bindingID)
	}
	result, err := s.Configuration.UpdateBinding(binding, func(current *domain.AgentBinding) {
		if len(current.PendingMessages) == 0 {
			current.PendingMessage = nil
			return
		}
		head := current.PendingMessages[0]
		if pending == nil && head == nil || pending != nil && head != nil && strings.TrimSpace(head.MessageID) == strings.TrimSpace(pending.MessageID) {
			current.PendingMessages = current.PendingMessages[1:]
			current.PendingMessage = nil
			if len(current.PendingMessages) > 0 {
				current.PendingMessage = current.PendingMessages[0]
			}
		}
	})
	return result.Effects, err
}
func (s PendingService) Discard(messageID string) (bool, error) {
	if s.Repository == nil {
		return false, fmt.Errorf("pending repository is nil")
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false, nil
	}
	discarded := false
	for _, binding := range s.Repository.AgentBindings() {
		if binding == nil {
			continue
		}
		matched := false
		_, err := s.Repository.UpdateAgentBinding(binding, func(current *domain.AgentBinding) {
			filtered := make([]*domain.AgentBindingPendingMessage, 0, len(current.PendingMessages))
			for _, pending := range current.PendingMessages {
				if pending != nil && strings.TrimSpace(pending.MessageID) == messageID {
					matched = true
					continue
				}
				filtered = append(filtered, pending)
			}
			if !matched {
				return
			}
			current.PendingMessages = filtered
			current.PendingMessage = nil
			if len(filtered) > 0 {
				current.PendingMessage = filtered[0]
			}
		})
		discarded = discarded || matched
		if err != nil {
			return discarded, err
		}
	}
	return discarded, nil
}

func PendingFromMessage(msg *application.InboundMessage, sessionKey string, storedAt int64) *domain.AgentBindingPendingMessage {
	if msg == nil {
		return nil
	}
	attachments := make([]domain.AgentBindingPendingAttachment, 0, len(msg.Attachments))
	for _, attachment := range msg.Attachments {
		attachments = append(attachments, domain.AgentBindingPendingAttachment{
			Kind:            attachment.Kind,
			ResourceKey:     attachment.ResourceKey,
			SourceMessageID: attachment.SourceMessageID,
		})
	}
	return &domain.AgentBindingPendingMessage{
		SessionKey:             sessionKey,
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
		StoredAt:               storedAt,
	}
}

func MessageFromPending(pending *domain.AgentBindingPendingMessage) *application.InboundMessage {
	if pending == nil {
		return nil
	}
	attachments := make([]application.Attachment, 0, len(pending.Attachments))
	for _, attachment := range pending.Attachments {
		attachments = append(attachments, application.Attachment{
			Kind:            attachment.Kind,
			ResourceKey:     attachment.ResourceKey,
			SourceMessageID: attachment.SourceMessageID,
		})
	}
	return &application.InboundMessage{
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
