package feishuapp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	appfeishuwrap "feidex/internal/adapter/feishu/feishuwrap"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

func TestComposedOutboundEffectsPreserveDeliveryAndCapture(t *testing.T) {
	base := &fakeFeishuClient{}
	original := newFeishuClient
	newFeishuClient = func(config.FeishuConfig) FeishuClient { return base }
	t.Cleanup(func() { newFeishuClient = original })
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	a, err := New(cfg, filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.feishu.(*appfeishuwrap.EffectClient); !ok {
		t.Fatal("frontend must expose a separate effect capability")
	}
	ctx := context.Background()
	card := map[string]any{"test": "card"}
	if err := a.feishu.ReplyText(ctx, "parent", "reply", true); err != nil {
		t.Fatal(err)
	}
	if id, err := a.feishu.ReplyTextWithID(ctx, "parent", "reply with id", true); err != nil || id != "reply-text-id" {
		t.Fatalf("reply text id = %q, %v", id, err)
	}
	if err := a.feishu.SendText(ctx, "chat", "send"); err != nil {
		t.Fatal(err)
	}
	if id, err := a.feishu.ReplyCard(ctx, "parent", card, true); err != nil || id != "reply-card-id" {
		t.Fatalf("reply card id = %q, %v", id, err)
	}
	if id, err := a.feishu.SendCard(ctx, "chat", card); err != nil || id != "send-card-id" {
		t.Fatalf("send card id = %q, %v", id, err)
	}
	if err := a.feishu.PatchCard(ctx, "card", card); err != nil {
		t.Fatal(err)
	}
	if len(base.replyTexts) != 1 || len(base.replyTextWithIDs) != 1 || len(base.sentTexts) != 1 || len(base.replyCards) != 1 || len(base.sendCards) != 1 || len(base.patchedCards) != 1 || !base.replyCardInThread[0] {
		t.Fatal("effects must reach the transport exactly once and retain thread routing")
	}
	capture := a.feishu.(appfeishuwrap.CommandCaptureFeishuClient)
	_, captured, err := capture.CaptureCommandOutput("parent", func() error {
		_, err := replyCardWithIDEffect(ctx, a, "parent", card, true)
		return err
	})
	if err != nil || captured["test"] != "card" || len(base.replyCards) != 1 {
		t.Fatalf("effect command capture = %#v, %v", captured, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.feishu.ReplyCard(cancelled, "parent", card, true); !errors.Is(err, context.Canceled) || len(base.replyCards) != 1 {
		t.Fatalf("cancelled delivery must not reach transport: %v", err)
	}
	base.replyCardErr = errors.New("transport failure")
	if _, err := a.feishu.ReplyCard(ctx, "parent", card, true); !errors.Is(err, base.replyCardErr) {
		t.Fatalf("transport failure was lost: %v", err)
	}
	base.replyTextErr = &permissionIssueTestError{
		err:   errors.New("permission denied"),
		issue: &feishu.PermissionIssue{API: "im.message.reply", Code: 99991668, Message: "permission denied"},
	}
	base.replyCardErr = nil
	before := len(base.replyCards)
	for range 2 {
		if err := a.feishu.ReplyText(ctx, "parent", "reply", true); !errors.Is(err, base.replyTextErr) {
			t.Fatalf("permission error was lost: %v", err)
		}
	}
	if len(base.replyCards) != before+1 || !strings.Contains(cardMarkdownContent(t, base.replyCards[before]), "im.message.reply") {
		t.Fatal("effect delivery must retain permission diagnostics and deduplication")
	}
}
