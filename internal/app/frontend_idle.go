package app

import (
	"strings"

	"feidex/internal/state"
)

func frontendIsIdle(a *App) bool {
	return frontendIdleBlockedReason(a) == ""
}

func frontendIdleBlockedReason(a *App) string {
	return frontendIdleBlockedReasonWithMessageTrafficAllowance(a, 0)
}

func frontendIdleBlockedReasonIgnoringCurrentMessage(a *App) string {
	return frontendIdleBlockedReasonWithMessageTrafficAllowance(a, 1)
}

func frontendIdleBlockedReasonWithMessageTrafficAllowance(a *App, allowedMessageTraffic int) string {
	if a == nil {
		return "app not initialized"
	}
	if reason := newRuntimeStateService(a).backendSwitchBlockedReasonForTraffic(); reason != "" {
		return reason
	}
	for _, runtime := range backendRuntimeFacades() {
		if runtime.maintenanceActive(a) {
			return runtime.idleMaintenanceBlockedReason()
		}
	}
	if newRuntimeStateService(a).frontendMessageTrafficCount() > allowedMessageTraffic {
		return "当前仍有消息处理中"
	}
	autoRetrySvc := newAutoRetryService(a)
	for _, sess := range a.State().Sessions() {
		if sess == nil || !sessionBelongsToFrontend(a, sess.Key) {
			continue
		}
		if sessionHasActiveWork(sess) {
			return "当前仍有运行中的任务"
		}
		if autoRetrySvc.HasBlockingAutoRetry(sess.Key) {
			return "当前仍有自动重试中的任务"
		}
		if len(sess.Queue) > 0 {
			return "当前仍有排队中的消息"
		}
		if len(sess.StagedImages) > 0 {
			return "当前仍有暂存图片待提交"
		}
		if state.NormalizeSessionStatus(firstNonEmpty(strings.TrimSpace(sess.Status), state.SessionStatusIdle.String())) != state.SessionStatusIdle {
			return "当前会话还没有完全回到空闲态"
		}
	}
	for _, req := range a.State().PendingRequests() {
		if isPendingRequestOpen(req) {
			return "当前仍有待处理审批或表单"
		}
	}
	return ""
}
