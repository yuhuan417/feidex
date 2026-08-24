package app

import (
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

type groupMessagePolicyConfigurer interface {
	SetGroupMessagePolicy(feishu.GroupMessagePolicy)
}

func configureGroupMessagePolicy(a *App) {
	if a == nil || a.feishu == nil {
		return
	}
	configurer, ok := a.feishu.(groupMessagePolicyConfigurer)
	if !ok {
		return
	}
	configurer.SetGroupMessagePolicy(func(chatID, rootMessageID, parentMessageID string, mentionedSelf, mentionedAny, mentionedEveryone bool) bool {
		return shouldAcceptGroupMessage(a, chatID, rootMessageID, parentMessageID, mentionedSelf, mentionedAny, mentionedEveryone)
	})
}

func hasLocalAgentBinding(a *App, chatID string) bool {
	if a == nil {
		return false
	}
	return len(a.State().AgentBindingsForChat("group", chatID)) > 0
}

func shouldAcceptGroupMessage(a *App, chatID, rootMessageID, parentMessageID string, mentionedSelf, mentionedAny, mentionedEveryone bool) bool {
	if a == nil {
		return false
	}
	cfg := feishuConfig(a)
	bindings := a.State().AgentBindingsForChat("group", chatID)
	activeBindings := make([]*state.AgentBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding == nil || !strings.EqualFold(strings.TrimSpace(binding.Status), state.AgentBindingStatusActive.String()) {
			continue
		}
		activeBindings = append(activeBindings, binding)
	}

	// Preserve the existing @-only behavior for chats that have not entered
	// binding mode yet, including pending onboarding bindings.
	if len(activeBindings) == 0 {
		return mentionedSelf || (mentionedEveryone && cfg != nil && cfg.RespondToAtEveryone)
	}
	if mentionedSelf {
		return true
	}
	// An explicit mention of another person or bot must not fall through to
	// the local primary binding.
	if mentionedAny || mentionedEveryone {
		if mentionedEveryone && cfg != nil && cfg.RespondToAtEveryone {
			return hasPrimaryBinding(activeBindings)
		}
		return false
	}
	if rootMessageID != "" || parentMessageID != "" {
		// Message links are frontend-scoped, so a local link proves that this
		// bot owns the reply chain. A missing link is intentionally ignored.
		if hasLocalGroupMessageLink(a, rootMessageID, parentMessageID) {
			return true
		}
		return false
	}
	return hasPrimaryBinding(activeBindings)
}

func hasLocalGroupMessageLink(a *App, messageIDs ...string) bool {
	if a == nil {
		return false
	}
	for _, messageID := range messageIDs {
		if strings.TrimSpace(messageID) == "" {
			continue
		}
		if link := a.State().MessageLink(messageID); link != nil {
			return true
		}
	}
	return false
}

func hasPrimaryBinding(bindings []*state.AgentBinding) bool {
	for _, binding := range bindings {
		if binding != nil && binding.Primary {
			return true
		}
	}
	return false
}
