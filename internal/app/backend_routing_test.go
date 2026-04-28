package app

import (
	"strings"
	"testing"

	"feidex/internal/feishu"
)

func TestHandleCompactCommandWithoutBackendDoesNotFallbackToCodex(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = ""
	a.cfg.Feishu.Backend = ""

	err := newBackendActionService(a).HandleCompactCommand(&feishu.InboundMessage{
		MessageID: "msg-1",
		ChatType:  "p2p",
		UserID:    "user-1",
		ChatID:    "chat-1",
	}, nil)
	if err == nil {
		t.Fatal("HandleCompactCommand() error = nil, want backend-not-configured error")
	}
	if strings.Contains(err.Error(), "compact runner not configured") {
		t.Fatalf("HandleCompactCommand() error = %q, should not fall back to Codex path", err)
	}
}

func TestMenuVisibilityWithoutBackendDoesNotExposeConversationMenu(t *testing.T) {
	if menuActionVisibleForBackend("menu.thread", "") {
		t.Fatal("menu.thread should be hidden when backend is unset")
	}
	if !menuActionVisibleForBackend("menu.group.backend", "") {
		t.Fatal("menu.group.backend should stay visible when backend is unset")
	}
}
