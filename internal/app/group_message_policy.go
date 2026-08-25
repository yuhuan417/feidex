package app

import (
	"strings"

	"feidex/internal/feishu"
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
	configurer.SetGroupMessagePolicy(func(input feishu.GroupMessagePolicyInput) bool {
		return shouldDeliverGroupMessageToApp(a, input)
	})
}

func shouldDeliverGroupMessageToApp(a *App, input feishu.GroupMessagePolicyInput) bool {
	mentionedAny := len(input.MentionedOpenIDs) > 0
	if shouldAcceptGroupMessage(a, input.ChatID, input.RootMessageID, input.ParentMessageID, input.MentionedSelf, mentionedAny, input.MentionedEveryone) {
		return true
	}
	if _, ok := groupPrimaryAssignmentFromPolicyInput(input); ok {
		return true
	}
	return shouldProbeGroupPrimaryForMessage(a, input.ChatID, input.RootMessageID, input.ParentMessageID, input.MentionedSelf, mentionedAny, input.MentionedEveryone)
}

func shouldProbeGroupPrimaryForMessage(a *App, chatID, rootMessageID, parentMessageID string, mentionedSelf, mentionedAny, mentionedEveryone bool) bool {
	if a == nil || hasGroupPrimaryState(a, "group", chatID) {
		return false
	}
	if mentionedSelf {
		return true
	}
	if mentionedAny {
		return false
	}
	if mentionedEveryone {
		cfg := feishuConfig(a)
		return cfg != nil && cfg.RespondToAtEveryone
	}
	return strings.TrimSpace(rootMessageID) == "" && strings.TrimSpace(parentMessageID) == ""
}

func shouldAcceptGroupMessage(a *App, chatID, rootMessageID, parentMessageID string, mentionedSelf, mentionedAny, mentionedEveryone bool) bool {
	if a == nil {
		return false
	}
	cfg := feishuConfig(a)
	if mentionedSelf {
		return true
	}
	// An explicit mention of another person or bot must not fall through to
	// the local primary frontend.
	if mentionedAny || mentionedEveryone {
		if mentionedEveryone && cfg != nil && cfg.RespondToAtEveryone {
			return isGroupPrimary(a, "group", chatID)
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
	return isGroupPrimary(a, "group", chatID)
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
