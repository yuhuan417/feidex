package app

import "feidex/internal/feishu"

func handleBackendMaintenanceBlock(a *App, raw string) error {
	if reason := newRuntimeStateService(a).backendSwitchBlockedReasonForTraffic(); reason != "" {
		return newUIWarningError(reason)
	}
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.maintenanceBlocksCommand(a, raw)
	}
	return nil
}

func backendMaintenanceBlocksInboundMessage(a *App) error {
	if reason := newRuntimeStateService(a).backendSwitchBlockedReasonForTraffic(); reason != "" {
		return newUIWarningError(reason)
	}
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.maintenanceBlocksCommand(a, "")
	}
	return nil
}

func handleBackendCompactCommand(a *App, msg *feishu.InboundMessage) error {
	if a == nil || msg == nil {
		return nil
	}
	svc := newCompactService(a)
	return newBackendActionService(a).HandleCompactCommand(msg, &svc)
}
