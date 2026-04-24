package app

import "feidex/internal/feishu"

func (a *App) handleBackendMaintenanceBlock(raw string) error {
	if reason := newRuntimeStateService(a).backendSwitchBlockedReasonForTraffic(); reason != "" {
		return newUIWarningError(reason)
	}
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.maintenanceBlocksCommand(a, raw)
	}
	return nil
}

func (a *App) backendMaintenanceBlocksInboundMessage() error {
	if reason := newRuntimeStateService(a).backendSwitchBlockedReasonForTraffic(); reason != "" {
		return newUIWarningError(reason)
	}
	if runtime := backendRuntime(a); runtime != nil {
		return runtime.maintenanceBlocksCommand(a, "")
	}
	return nil
}

func (a *App) handleBackendCompactCommand(msg *feishu.InboundMessage) error {
	if a == nil || msg == nil {
		return nil
	}
	if actions := backendActions(a); actions != nil {
		return actions.handleCompactCommand(a, msg)
	}
	return nil
}
