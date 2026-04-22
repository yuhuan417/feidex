package app

import (
	"context"

	"feidex/internal/feishu"
)

func (a *App) handleBackendMaintenanceBlock(raw string) error {
	switch a.configuredBackend() {
	case backendCodex:
		return a.codexMaintenanceBlocksCommand(raw)
	case backendClaude:
		return a.claudeMaintenanceBlocksCommand(raw)
	default:
		return nil
	}
}

func (a *App) backendMaintenanceBlocksInboundMessage() error {
	switch a.configuredBackend() {
	case backendCodex:
		if a.codexMaintenanceActive() {
			return errString("Codex 正在维护中，当前只允许 `/codex`、`/status`、`/help`")
		}
	case backendClaude:
		if a.claudeMaintenanceActive() {
			return errString("Claude 正在维护中，当前只允许 `/claude`、`/status`、`/help`")
		}
	}
	return nil
}

func (a *App) handleBackendCompactCommand(msg *feishu.InboundMessage) error {
	if a == nil || msg == nil {
		return nil
	}
	if a.isClaudeBackend() {
		return a.enqueuePassthroughCommand(msg, "/compact")
	}
	if _, err := a.startThreadCompaction(a.makeSessionKey(msg)); err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已请求压缩当前线程上下文。", a.replyInThreadEnabled(msg.ChatType))
}
