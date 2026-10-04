package feishuapp

import (
	"context"
	"testing"

	"feidex/internal/application"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
)

func TestGoalCommandOutboundUsesFrontendScopedEffects(t *testing.T) {
	var replyCard application.SendCard
	var sendCard application.SendCard
	var replyText application.SendMessage
	runner := frontendruntime.EffectRunner{
		SendCardWithID: func(_ context.Context, effect application.SendCard) (string, error) {
			if effect.ReplyMessageID != "" {
				replyCard = effect
			} else {
				sendCard = effect
			}
			return "sent-id", nil
		},
		Send: func(_ context.Context, effect application.SendMessage) error {
			replyText = effect
			return nil
		},
	}
	outbound := GoalCommandOutbound(identity.FrontendID("frontend-a"), runner)

	if id, err := outbound.ReplyCard(context.Background(), "parent-1", map[string]any{"title": "reply"}, true); err != nil || id != "sent-id" {
		t.Fatalf("ReplyCard() = (%q, %v)", id, err)
	}
	if err := outbound.ReplyText(context.Background(), "parent-2", "text", false); err != nil {
		t.Fatalf("ReplyText() error = %v", err)
	}
	if id, err := outbound.SendCard(context.Background(), "chat-1", map[string]any{"title": "send"}); err != nil || id != "sent-id" {
		t.Fatalf("SendCard() = (%q, %v)", id, err)
	}

	if replyCard.Frontend != "frontend-a" || replyCard.ReplyMessageID != "parent-1" || !replyCard.InThread {
		t.Fatalf("reply card effect = %+v", replyCard)
	}
	if replyText.Frontend != "frontend-a" || replyText.ReplyMessageID != "parent-2" || replyText.Text != "text" {
		t.Fatalf("reply text effect = %+v", replyText)
	}
	if sendCard.Frontend != "frontend-a" || sendCard.Chat.ID != "chat-1" || sendCard.ReplyMessageID != "" {
		t.Fatalf("send card effect = %+v", sendCard)
	}
}

func TestGoalCommandSessionKeyPreservesFrontendAndLegacyFormats(t *testing.T) {
	if got := GoalCommandSessionKey("frontend-a", &feishu.InboundMessage{ChatID: " chat-1 "}); got != "feishu:frontend:frontend-a:chat:chat-1" {
		t.Fatalf("new session key = %q", got)
	}
	if got := GoalCommandSessionKey("", &feishu.InboundMessage{ChatID: "chat-1"}); got != "feishu:chat:chat-1" {
		t.Fatalf("legacy session key = %q", got)
	}
	if got := GoalCommandSessionKey("frontend-a", &feishu.InboundMessage{SessionKey: "feishu:chat:chat-1"}); got != "feishu:frontend:frontend-a:chat:chat-1" {
		t.Fatalf("canonicalized session key = %q", got)
	}
}
