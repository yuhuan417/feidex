package outbound

import (
	"context"
	"fmt"

	"feidex/internal/application"
	"feidex/internal/runtime"
)

// Client is the Feishu transport slice required by application effects. The
// adapter owns SDK-facing behavior; application and runtime only see semantic
// effect values.
type Client interface {
	ReplyText(context.Context, string, string, bool) error
	ReplyTextWithID(context.Context, string, string, bool) (string, error)
	SendText(context.Context, string, string) error
	ReplyCard(context.Context, string, map[string]any, bool) (string, error)
	SendCard(context.Context, string, map[string]any) (string, error)
	PatchCard(context.Context, string, map[string]any) error
}

func NewEffectRunner(client Client) runtime.EffectRunner {
	return runtime.EffectRunner{
		Send: func(ctx context.Context, e application.SendMessage) error {
			if client == nil {
				return fmt.Errorf("feishu outbound unavailable")
			}
			if e.ReplyMessageID != "" {
				return client.ReplyText(ctx, e.ReplyMessageID, e.Text, e.InThread)
			}
			return client.SendText(ctx, e.Chat.ID, e.Text)
		},
		SendWithID: func(ctx context.Context, e application.SendMessage) (string, error) {
			if client == nil {
				return "", fmt.Errorf("feishu outbound unavailable")
			}
			if e.ReplyMessageID != "" {
				return client.ReplyTextWithID(ctx, e.ReplyMessageID, e.Text, e.InThread)
			}
			return "", client.SendText(ctx, e.Chat.ID, e.Text)
		},
		SendCard: func(ctx context.Context, e application.SendCard) error {
			card, err := Render(e.View)
			if err != nil {
				return err
			}
			if client == nil {
				return fmt.Errorf("feishu outbound unavailable")
			}
			if e.ReplyMessageID != "" {
				_, err = client.ReplyCard(ctx, e.ReplyMessageID, card, e.InThread)
				return err
			}
			_, err = client.SendCard(ctx, e.Chat.ID, card)
			return err
		},
		SendCardWithID: func(ctx context.Context, e application.SendCard) (string, error) {
			card, err := Render(e.View)
			if err != nil {
				return "", err
			}
			if client == nil {
				return "", fmt.Errorf("feishu outbound unavailable")
			}
			if e.ReplyMessageID != "" {
				return client.ReplyCard(ctx, e.ReplyMessageID, card, e.InThread)
			}
			return client.SendCard(ctx, e.Chat.ID, card)
		},
		Patch: func(ctx context.Context, e application.PatchCard) error {
			card, err := Render(e.View)
			if err != nil {
				return err
			}
			if client == nil {
				return fmt.Errorf("feishu outbound unavailable")
			}
			return client.PatchCard(ctx, e.MessageID, card)
		},
	}
}
