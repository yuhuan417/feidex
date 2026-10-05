package feishuapp

import (
	"context"
	"encoding/json"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"strings"

	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// replyCardEffect routes a user-visible card through the application effect
// runner. Callers keep ownership of rendering while transport details stay at
// the composition boundary.
func replyCardEffect(runner frontendruntime.EffectRunner, frontendID string, inThread bool, msg *feishu.InboundMessage, card map[string]any) error {
	if msg == nil {
		return nil
	}
	return runner.Run(context.Background(), []application.Effect{application.SendCard{
		Frontend:       identity.FrontendID(frontendID),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		View:           feishuoutbound.Card(card),
		InThread:       inThread,
	}})
}

func replyTextEffect(runner frontendruntime.EffectRunner, frontendID string, inThread bool, msg *feishu.InboundMessage, text string) error {
	if msg == nil {
		return nil
	}
	return runner.Run(context.Background(), []application.Effect{application.SendMessage{
		Frontend:       identity.FrontendID(frontendID),
		Chat:           identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)},
		ReplyMessageID: msg.MessageID,
		Text:           text,
		InThread:       inThread,
	}})
}

func replyCommandActionResponseWith(runner frontendruntime.EffectRunner, frontendID string, inThread bool, msg *feishu.InboundMessage, resp *callback.CardActionTriggerResponse) error {
	if msg == nil || resp == nil {
		return nil
	}
	frontend := identity.FrontendID(frontendID)
	chat := identity.ChatRef{ID: msg.ChatID, Type: identity.ChatType(msg.ChatType)}
	if resp.Card != nil {
		if card, ok := resp.Card.Data.(map[string]any); ok && len(card) > 0 {
			return runner.Run(context.Background(), []application.Effect{application.SendCard{
				Frontend: frontend, Chat: chat, ReplyMessageID: msg.MessageID,
				View: feishuoutbound.Card(card), InThread: inThread,
			}})
		}
	}
	if resp.Toast != nil && strings.TrimSpace(resp.Toast.Content) != "" {
		return runner.Run(context.Background(), []application.Effect{application.SendMessage{
			Frontend: frontend, Chat: chat, ReplyMessageID: msg.MessageID,
			Text: strings.TrimSpace(resp.Toast.Content), InThread: inThread,
		}})
	}
	return nil
}

func replyCardWithIDEffect(ctx context.Context, runner frontendruntime.EffectRunner, frontendID, parentMessageID string, card map[string]any, inThread bool) (string, error) {
	return newEffectOutbound(frontendID, runner).ReplyCard(ctx, parentMessageID, card, inThread)
}

func sendCardWithIDEffect(ctx context.Context, runner frontendruntime.EffectRunner, frontendID, chatID string, card map[string]any) (string, error) {
	return newEffectOutbound(frontendID, runner).SendCard(ctx, chatID, card)
}

func patchCardEffect(ctx context.Context, runner frontendruntime.EffectRunner, frontendID, messageID string, card map[string]any) error {
	return newEffectOutbound(frontendID, runner).PatchCard(ctx, messageID, card)
}

func cardEffectKey(kind, frontendID, target string, card map[string]any) string {
	data, err := json.Marshal(card)
	if err != nil {
		return ""
	}
	return application.StableEffectKey(kind, frontendID, target, string(data))
}
