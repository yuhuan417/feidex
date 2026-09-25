package app

import (
	"context"
	"strings"
	"testing"

	appclauderuntime "feidex/internal/app/clauderuntime"
	"feidex/internal/claudecli"
)

func TestSendClaudeBackgroundTaskNotificationRepliesWithStatusCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sendClaudeBackgroundTaskNotification(a, context.Background(), appclauderuntime.BackgroundTaskTarget{
		SessionKey:       "sess-1",
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		TriggerMessageID: "trigger-1",
		ChatID:           "chat-1",
	}, claudecli.BackgroundTaskEvent{
		Status:      "completed",
		Description: "inspect repository",
		Summary:     "all clear",
	})

	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
	if got := cardHeaderTitle(t, ff.replyCards[0]); !strings.Contains(got, "后台 Agent 已完成") {
		t.Fatalf("card title = %q", got)
	}
	body := cardMarkdownContent(t, ff.replyCards[0])
	if !strings.Contains(body, "inspect repository") || !strings.Contains(body, "all clear") || !strings.Contains(body, "completed") {
		t.Fatalf("card body = %q", body)
	}
}

func TestSendClaudeBackgroundTaskNotificationFallsBackToChatCard(t *testing.T) {
	a, ff, _ := newTestApp(t)
	ff.replyCardErr = context.Canceled
	sendClaudeBackgroundTaskNotification(a, context.Background(), appclauderuntime.BackgroundTaskTarget{
		TriggerMessageID: "expired-trigger",
		ChatID:           "chat-1",
	}, claudecli.BackgroundTaskEvent{Status: "failed", Summary: "agent stopped"})

	if len(ff.sendCards) != 1 || len(ff.sendCardChatIDs) != 1 || ff.sendCardChatIDs[0] != "chat-1" {
		t.Fatalf("fallback send cards = %d, chats = %#v", len(ff.sendCards), ff.sendCardChatIDs)
	}
	if got := cardHeaderTitle(t, ff.sendCards[0]); !strings.Contains(got, "后台 Agent 未完成") {
		t.Fatalf("fallback card title = %q", got)
	}
}
