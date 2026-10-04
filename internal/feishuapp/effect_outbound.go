package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/runtime"
)

type effectOutbound struct {
	frontend identity.FrontendID
	runner   runtime.EffectRunner
}

func (o effectOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return o.runner.RunSendCard(ctx, application.SendCard{
		Frontend: o.frontend, ReplyMessageID: messageID, View: outbound.Card(card), InThread: inThread,
	})
}

func (o effectOutbound) ReplyInteractionCard(ctx context.Context, requestID, messageID string, card map[string]any, inThread bool) (string, error) {
	return o.runner.RunSendCard(ctx, application.SendCard{
		Frontend: o.frontend, ReplyMessageID: messageID, View: outbound.Card(card), InThread: inThread,
		IdempotencyKey: application.StableEffectKey("interaction-card", string(o.frontend), requestID),
	})
}

func (o effectOutbound) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	return o.runner.Run(ctx, []application.Effect{application.SendMessage{
		Frontend: o.frontend, ReplyMessageID: messageID, Text: text, InThread: inThread,
	}})
}

func (o effectOutbound) ReplyTextWithID(ctx context.Context, messageID, text string, inThread bool) (string, error) {
	return o.runner.RunSendMessage(ctx, application.SendMessage{
		Frontend: o.frontend, ReplyMessageID: messageID, Text: text, InThread: inThread,
	})
}

func (o effectOutbound) SendCard(ctx context.Context, chatID string, card map[string]any) (string, error) {
	return o.runner.RunSendCard(ctx, application.SendCard{
		Frontend: o.frontend, Chat: identity.ChatRef{ID: chatID}, View: outbound.Card(card),
	})
}

func (o effectOutbound) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	return o.runner.Run(ctx, []application.Effect{application.PatchCard{
		Frontend: o.frontend, MessageID: messageID, View: outbound.Card(card),
		IdempotencyKey: cardEffectKey("patch-card", string(o.frontend), messageID, card),
	}})
}

func newEffectOutbound(frontend string, runner runtime.EffectRunner) effectOutbound {
	return effectOutbound{frontend: identity.FrontendID(frontend), runner: runner}
}
