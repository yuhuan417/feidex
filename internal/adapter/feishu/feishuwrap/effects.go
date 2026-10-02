package feishuwrap

import (
	"context"

	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/runtime"
)

// EffectClient is the business-facing outbound capability. Its runner is
// bound to NotifyingFeishuClient, never back to this proxy. Embedding preserves
// transport operations, command capture, permissions and optional group hooks.
type EffectClient struct {
	*NotifyingFeishuClient
	Frontend identity.FrontendID
	Runner   runtime.EffectRunner
}

func (c *EffectClient) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	return c.Runner.Run(ctx, []application.Effect{application.SendMessage{
		Frontend: c.Frontend, ReplyMessageID: messageID, Text: text, InThread: inThread,
	}})
}

func (c *EffectClient) ReplyTextWithID(ctx context.Context, messageID, text string, inThread bool) (string, error) {
	return c.Runner.RunSendMessage(ctx, application.SendMessage{
		Frontend: c.Frontend, ReplyMessageID: messageID, Text: text, InThread: inThread,
	})
}

func (c *EffectClient) SendText(ctx context.Context, chatID, text string) error {
	return c.Runner.Run(ctx, []application.Effect{application.SendMessage{
		Frontend: c.Frontend, Chat: identity.ChatRef{ID: chatID}, Text: text,
	}})
}

func (c *EffectClient) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return c.Runner.RunSendCard(ctx, application.SendCard{
		Frontend: c.Frontend, ReplyMessageID: messageID, View: card, InThread: inThread,
	})
}

func (c *EffectClient) SendCard(ctx context.Context, chatID string, card map[string]any) (string, error) {
	return c.Runner.RunSendCard(ctx, application.SendCard{
		Frontend: c.Frontend, Chat: identity.ChatRef{ID: chatID}, View: card,
	})
}

func (c *EffectClient) PatchCard(ctx context.Context, messageID string, card map[string]any) error {
	return c.Runner.Run(ctx, []application.Effect{application.PatchCard{
		Frontend: c.Frontend, MessageID: messageID, View: card,
	}})
}
