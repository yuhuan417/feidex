package feishuapp

import "log/slog"

func bindClaudeSessionThread(a *App, sessionKey, turnID, threadID string) {
	if a == nil {
		return
	}
	if err := a.bindings.Conversations.BindBackendSessionThread(sessionKey, turnID, threadID, "Claude"); err != nil {
		slog.Error("backend session thread binding failed", "session_key", sessionKey, "turn_id", turnID, "thread_id", threadID, "error", err)
	}
}
