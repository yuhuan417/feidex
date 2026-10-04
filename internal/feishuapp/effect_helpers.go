package feishuapp

import (
	"context"
	"encoding/json"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"

	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
)

// replyCardEffect routes a user-visible card through the application effect
// runner. Callers keep ownership of rendering while transport details stay at
// the composition boundary.
func replyCardEffect(a *App, msg *feishu.InboundMessage, card map[string]any) error {
	if a == nil || msg == nil {
		return nil
	}
	return newEffectRunner(a.runtimeOwner).Run(context.Background(), []application.Effect{application.SendCard{
		Frontend:       identity.FrontendID(a.FrontendID()),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		View:           feishuoutbound.Card(card),
		InThread:       a.configView().replyInThreadEnabled(),
	}})
}

func replyTextEffect(a *App, msg *feishu.InboundMessage, text string) error {
	if a == nil || msg == nil {
		return nil
	}
	return newEffectRunner(a.runtimeOwner).Run(context.Background(), []application.Effect{application.SendMessage{
		Frontend:       identity.FrontendID(a.FrontendID()),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		Text:           text,
		InThread:       a.configView().replyInThreadEnabled(),
	}})
}

func replyCardWithIDEffect(ctx context.Context, a *App, parentMessageID string, card map[string]any, inThread bool) (string, error) {
	if a == nil {
		return "", nil
	}
	return newEffectRunner(a.runtimeOwner).RunSendCard(ctx, application.SendCard{Frontend: identity.FrontendID(a.FrontendID()), ReplyMessageID: parentMessageID, View: feishuoutbound.Card(card), InThread: inThread})
}

func sendCardWithIDEffect(ctx context.Context, a *App, chatID string, card map[string]any) (string, error) {
	if a == nil {
		return "", nil
	}
	return newEffectRunner(a.runtimeOwner).RunSendCard(ctx, application.SendCard{Frontend: identity.FrontendID(a.FrontendID()), Chat: identity.ChatRef{ID: chatID}, View: feishuoutbound.Card(card)})
}

func patchCardEffect(ctx context.Context, a *App, messageID string, card map[string]any) error {
	if a == nil {
		return nil
	}
	return newEffectRunner(a.runtimeOwner).Run(ctx, []application.Effect{application.PatchCard{
		Frontend:       identity.FrontendID(a.FrontendID()),
		MessageID:      messageID,
		View:           feishuoutbound.Card(card),
		IdempotencyKey: cardEffectKey("patch-card", a.FrontendID(), messageID, card),
	}})
}

func cardEffectKey(kind, frontendID, target string, card map[string]any) string {
	data, err := json.Marshal(card)
	if err != nil {
		return ""
	}
	return application.StableEffectKey(kind, frontendID, target, string(data))
}
