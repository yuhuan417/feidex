package feishuapp

import (
	"feidex/internal/application"
	"feidex/internal/application/routing"

	domainrouting "feidex/internal/domain/routing"
	"feidex/internal/feishu"
)

func groupPolicyRootMessageID(msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	return application.GroupPolicyRootMessageID(msg.MessageID, msg.RootMessageID, msg.ParentMessageID)
}

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
		return shouldDeliverGroupMessageToApp(a.bindings.GroupMessages, input)
	})
}

func shouldDeliverGroupMessageToApp(groupmessages routing.GroupMessages, input feishu.GroupMessagePolicyInput) bool {
	return groupmessages.Deliver(routing.GroupMessage{
		ChatID: input.ChatID, RootMessageID: input.RootMessageID, ParentMessageID: input.ParentMessageID,
		Text: input.Text, MentionedOpenIDs: input.MentionedOpenIDs, MentionAll: input.MentionAll,
		MentionedSelf: input.MentionedSelf, MentionedAny: input.MentionedAny,
	})
}

func messageMentionsCurrentBot(a *App, mentionedOpenIDs []string, fallback bool) bool {
	return domainrouting.MentionsSelf(currentLiveBotOpenID(a), mentionedOpenIDs, fallback)
}

func shouldAcceptGroupMessage(a *App, chatID, rootMessageID, parentMessageID string, mentionedSelf, mentionedAny bool) bool {
	if a == nil {
		return false
	}
	return a.bindings.GroupMessages.Accept(routing.GroupMessage{
		ChatID: chatID, RootMessageID: rootMessageID, ParentMessageID: parentMessageID,
		MentionedSelf: mentionedSelf,
		MentionedAny:  mentionedAny,
	})
}
