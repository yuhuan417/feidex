package app

import (
	"context"
	"log/slog"
	"strings"
)

// Claude runtime applies that are too slow for a card callback (stopping or
// restarting the CLI process, control-request round-trips) run here, after the
// callback has been answered. Failures are reported back to the conversation
// instead of being lost with the discarded ack.

// resetClaudeSessionAfterAuxModelChange restarts the Claude session off the
// card callback ack path. Stopping the CLI process is blocking I/O, and
// auxiliary model changes only take effect after a restart.
func resetClaudeSessionAfterAuxModelChange(a *App, messageID, sessionKey string) {
	if a == nil || a.claude == nil {
		return
	}
	runAsync(a, func() {
		if err := a.claude.ResetSession(sessionKey); err != nil {
			notifyClaudeRuntimeApplyFailure(a, messageID, sessionKey, "会话重启失败", err)
		}
	})
}

// notifyClaudeRuntimeApplyFailure reports a runtime apply that failed after the
// card callback had already been answered.
func notifyClaudeRuntimeApplyFailure(a *App, messageID, sessionKey, label string, err error) {
	if a == nil || err == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	messageID = strings.TrimSpace(messageID)
	slog.Warn("claude runtime apply failed after card ack",
		"session_key", sessionKey,
		"message_id", messageID,
		"label", label,
		"error", err,
	)
	if a.feishu == nil || messageID == "" {
		return
	}
	replyInThread := false
	if sess := a.State().Session(sessionKey); sess != nil {
		replyInThread = replyInThreadEnabled(a, sess.ChatType)
	}
	text := "⚠️ " + label + "：" + err.Error() + "\n配置已保存，可用 /claude restart 重试当前会话。"
	if replyErr := a.feishu.ReplyText(context.Background(), messageID, text, replyInThread); replyErr != nil {
		slog.Warn("report claude runtime apply failure failed",
			"session_key", sessionKey,
			"message_id", messageID,
			"error", replyErr,
		)
	}
}
