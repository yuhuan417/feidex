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
	mentionedAny := input.MentionedAny || len(input.MentionedOpenIDs) > 0
	mentionedSelf := messageMentionsCurrentBot(a, input.MentionedOpenIDs, input.MentionedSelf)
	if isGroupPrimaryControlInput(input) {
		// A primary assignment must contain exactly one mention. A bare
		// /primary on or a multi-mention assignment is ambiguous and must not
		// be delivered to every frontend.
		_, ok := groupPrimaryAssignmentFromPolicyInput(input)
		return ok
	}
	// @所有人 addresses the whole group, so every bot answers it rather than only
	// the primary one. Without this the message would fall through to
	// isGroupPrimary below and only the primary bot would reply.
	if input.MentionAll {
		return true
	}
	if shouldAcceptGroupMessage(a, input.ChatID, input.RootMessageID, input.ParentMessageID, mentionedSelf, mentionedAny) {
		return true
	}
	// Every frontend must see a primary assignment so each frontend can update
	// its own local primary state from the same Feishu event.
	if _, ok := groupPrimaryAssignmentFromPolicyInput(input); ok {
		return true
	}
	return shouldProbeGroupPrimaryForMessage(a, input.ChatID, input.RootMessageID, input.ParentMessageID, mentionedSelf, mentionedAny)
}

func groupPrimaryAssignmentFromPolicyInput(input feishu.GroupMessagePolicyInput) (groupPrimaryAssignment, bool) {
	return groupPrimaryAssignmentFromTextAndMentions(input.Text, input.MentionedOpenIDs)
}

func isGroupPrimaryControlInput(input feishu.GroupMessagePolicyInput) bool {
	// @所有人 is not a primary assignment. A bare "@_all" looks like a bare bot
	// mention to parseEmptyBotMentionFromText, but it names nobody, so the
	// assignment branch would swallow it and no bot would answer at all.
	if input.MentionAll {
		return false
	}
	return parsePrimaryOnCommandFromText(input.Text) || parseEmptyBotMentionFromText(input.Text)
}

func messageMentionsCurrentBot(a *App, mentionedOpenIDs []string, fallback bool) bool {
	selfOpenID := currentLiveBotOpenID(a)
	if selfOpenID == "" {
		return fallback
	}
	if len(mentionedOpenIDs) == 0 {
		return fallback
	}
	for _, mentionedOpenID := range mentionedOpenIDs {
		if strings.TrimSpace(mentionedOpenID) == selfOpenID {
			return true
		}
	}
	return false
}

func shouldProbeGroupPrimaryForMessage(a *App, chatID, rootMessageID, parentMessageID string, mentionedSelf, mentionedAny bool) bool {
	if a == nil || hasGroupPrimaryState(a, "group", chatID) {
		return false
	}
	if mentionedSelf {
		return true
	}
	if mentionedAny {
		return false
	}
	return strings.TrimSpace(rootMessageID) == "" && strings.TrimSpace(parentMessageID) == ""
}

func shouldAcceptGroupMessage(a *App, chatID, rootMessageID, parentMessageID string, mentionedSelf, mentionedAny bool) bool {
	if a == nil {
		return false
	}
	if mentionedSelf {
		return true
	}
	// An explicit mention of another person or bot must not fall through to the
	// local primary frontend.
	if mentionedAny {
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
