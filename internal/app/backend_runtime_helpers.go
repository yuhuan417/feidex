package app

import (
	"context"

	"feidex/internal/feishu"
)

func (a *App) handleBackendMaintenanceBlock(raw string) error {
	if reason := a.backendSwitchBlockedReasonForTraffic(); reason != "" {
		return newUIWarningError(reason)
	}
	if runtime := a.backendRuntime(); runtime != nil {
		return runtime.maintenanceBlocksCommand(a, raw)
	}
	return nil
}

func (a *App) backendMaintenanceBlocksInboundMessage() error {
	if reason := a.backendSwitchBlockedReasonForTraffic(); reason != "" {
		return newUIWarningError(reason)
	}
	if runtime := a.backendRuntime(); runtime != nil {
		return runtime.maintenanceBlocksCommand(a, "")
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
