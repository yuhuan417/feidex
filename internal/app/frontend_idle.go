package app

import "strings"

func (a *App) frontendIsIdle() bool {
	return a.frontendIdleBlockedReason() == ""
}

func (a *App) frontendIdleBlockedReason() string {
	if a == nil {
		return "app not initialized"
	}
	for _, runtime := range backendRuntimeFacades() {
		if runtime.maintenanceActive(a) {
			return runtime.idleMaintenanceBlockedReason()
		}
	}
	for _, sess := range a.appState().sessions() {
		if sess == nil || !a.sessionBelongsToFrontend(sess.Key) {
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
	for _, req := range a.appState().pendingRequests() {
		if isPendingRequestOpen(req) {
			return "当前仍有待处理审批或表单"
		}
	}
	return ""
}
