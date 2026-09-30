package app

import "fmt"

// Model writes change desired settings only. Active work, queues, images and
// open forms do not block saving; backend replacement still does.
func modelConfigBlockedReason(a *App) string {
	if a == nil {
		return ""
	}
	if reason := newRuntimeStateService(a).backendSwitchBlockedReasonForTraffic(); reason != "" {
		return reason
	}
	for _, runtime := range backendRuntimeFacades() {
		if runtime.maintenanceActive(a) {
			return runtime.idleMaintenanceBlockedReason()
		}
	}
	return ""
}

func ensureSessionModelConfigWritable(a *App, _ string) error {
	if reason := modelConfigBlockedReason(a); reason != "" {
		return fmt.Errorf("模型配置暂不可保存: %s", reason)
	}
	return nil
}
