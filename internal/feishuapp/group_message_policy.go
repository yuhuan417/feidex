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

func configureGroupMessagePolicy(client FeishuClient, groupMessages routing.GroupMessages) {
	if client == nil {
		return
	}
	configurer, ok := client.(groupMessagePolicyConfigurer)
	if !ok {
		return
	}
	configurer.SetGroupMessagePolicy(func(input feishu.GroupMessagePolicyInput) bool {
		return shouldDeliverGroupMessageToApp(groupMessages, input)
	})
}

func shouldDeliverGroupMessageToApp(groupmessages routing.GroupMessages, input feishu.GroupMessagePolicyInput) bool {
	return groupmessages.Deliver(routing.GroupMessage{
		ChatID: input.ChatID, RootMessageID: input.RootMessageID, ParentMessageID: input.ParentMessageID,
		Text: input.Text, MentionedOpenIDs: input.MentionedOpenIDs, MentionAll: input.MentionAll,
		MentionedSelf: input.MentionedSelf, MentionedAny: input.MentionedAny,
	})
}

func messageMentionsCurrentBot(client FeishuClient, mentionedOpenIDs []string, fallback bool) bool {
	return domainrouting.MentionsSelf(currentLiveBotOpenID(client), mentionedOpenIDs, fallback)
}

func shouldAcceptGroupMessage(groupMessages routing.GroupMessages, chatID, rootMessageID, parentMessageID string, mentionedSelf, mentionedAny bool) bool {
	return groupMessages.Accept(routing.GroupMessage{
		ChatID: chatID, RootMessageID: rootMessageID, ParentMessageID: parentMessageID,
		MentionedSelf: mentionedSelf,
		MentionedAny:  mentionedAny,
	})
}
