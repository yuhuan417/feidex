package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/runtime"
	"testing"
)

func TestEffectOutboundPreservesEffectTargetsAndKeys(t *testing.T) {
	var cards []application.SendCard
	var messages []application.SendMessage
	var patches []application.PatchCard
	runner := runtime.EffectRunner{
		SendCardWithID: func(_ context.Context, effect application.SendCard) (string, error) {
			cards = append(cards, effect)
			return "sent", nil
		},
		Send: func(_ context.Context, effect application.SendMessage) error {
			messages = append(messages, effect)
			return nil
		},
		Patch: func(_ context.Context, effect application.PatchCard) error {
			patches = append(patches, effect)
			return nil
		},
	}
	out := newEffectOutbound("frontend-1", runner)
	ctx := context.Background()
	card := map[string]any{"title": "status"}

	if id, err := out.ReplyCard(ctx, "reply-anchor", card, true); err != nil || id != "sent" {
		t.Fatalf("ReplyCard() = (%q, %v), want (sent, nil)", id, err)
	}
	if id, err := out.ReplyInteractionCard(ctx, "request-1", "interaction-anchor", card, false); err != nil || id != "sent" {
		t.Fatalf("ReplyInteractionCard() = (%q, %v), want (sent, nil)", id, err)
	}
	if err := out.ReplyText(ctx, "text-anchor", "message", true); err != nil {
		t.Fatalf("ReplyText() error = %v", err)
	}
	if id, err := out.SendCard(ctx, "chat-1", card); err != nil || id != "sent" {
		t.Fatalf("SendCard() = (%q, %v), want (sent, nil)", id, err)
	}
	if err := out.PatchCard(ctx, "patch-1", card); err != nil {
		t.Fatalf("PatchCard() error = %v", err)
	}

	frontend := identity.FrontendID("frontend-1")
	if len(cards) != 3 {
		t.Fatalf("card effects = %d, want 3", len(cards))
	}
	if got := cards[0]; got.Frontend != frontend || got.ReplyMessageID != "reply-anchor" || !got.InThread || got.View.(outbound.RenderedCard).Payload["title"] != "status" {
		t.Fatalf("reply card effect = %+v", got)
	}
	if got := cards[1]; got.Frontend != frontend || got.ReplyMessageID != "interaction-anchor" || got.IdempotencyKey != application.StableEffectKey("interaction-card", "frontend-1", "request-1") {
		t.Fatalf("interaction card effect = %+v", got)
	}
	if got := cards[2]; got.Frontend != frontend || got.Chat.ID != "chat-1" || got.ReplyMessageID != "" {
		t.Fatalf("send card effect = %+v", got)
	}
	if len(messages) != 1 || messages[0].Frontend != frontend || messages[0].ReplyMessageID != "text-anchor" || messages[0].Text != "message" || !messages[0].InThread {
		t.Fatalf("message effects = %+v", messages)
	}
	if len(patches) != 1 || patches[0].Frontend != frontend || patches[0].MessageID != "patch-1" || patches[0].IdempotencyKey != cardEffectKey("patch-card", "frontend-1", "patch-1", card) {
		t.Fatalf("patch effects = %+v", patches)
	}
}
