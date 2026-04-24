package app

import "strings"

func (a *App) frontendIsIdle() bool {
	return a.frontendIdleBlockedReason() == ""
}

func (a *App) frontendIdleBlockedReason() string {
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
	if newRuntimeStateService(a).frontendMessageTrafficCount() > 0 {
		return "当前仍有消息处理中"
	}
	for _, sess := range appState(a).sessions() {
		if sess == nil || !sessionBelongsToFrontend(a, sess.Key) {
			continue
		}
		if sessionHasActiveWork(sess) {
			return "当前仍有运行中的任务"
		}
		if len(sess.Queue) > 0 {
			return "当前仍有排队中的消息"
		}
		if len(sess.StagedImages) > 0 {
			return "当前仍有暂存图片待提交"
		}
		if firstNonEmpty(strings.TrimSpace(sess.Status), "idle") != "idle" {
			return "当前会话还没有完全回到空闲态"
		}
	}
	if newAutoRetryService(a).hasPendingAutoRetry("") {
		return "当前仍有等待自动重试的任务"
	}
	for _, req := range appState(a).pendingRequests() {
		if isPendingRequestOpen(req) {
			return "当前仍有待处理审批或表单"
		}
	}
	return ""
}
