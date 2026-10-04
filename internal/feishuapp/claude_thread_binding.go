package feishuapp

import (
	"log/slog"

	"feidex/internal/application/conversation"
)

func bindClaudeSessionThread(conversations *conversation.Service, sessionKey, turnID, threadID string) {
	if conversations == nil {
		return
	}
	if err := conversations.BindBackendSessionThread(sessionKey, turnID, threadID, "Claude"); err != nil {
		slog.Error("backend session thread binding failed", "session_key", sessionKey, "turn_id", turnID, "thread_id", threadID, "error", err)
	}
}
