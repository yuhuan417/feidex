package feishuapp

import (
	"context"
	"feidex/internal/runtime/maintenance"
	"time"
)

func (a *Frontend) Prepare(ctx context.Context) error {
	a.beginLifecycle(ctx)
	if err := startMCPService(a.runtimeOwner, a.BackendRuntimeDeps(), a.Context()); err != nil {
		runtimeViewOf(a.runtimeOwner).ensureRuntimeOwner().Lifecycle.Cancel()
		return err
	}
	if err := startPreparedBackendRuntime(a.BackendRuntimeDeps(), a.Context(), currentBackendRuntimeHandle(a.configView().configuredBackend(), runtimeViewOf(a.runtimeOwner))); err != nil {
		runtimeViewOf(a.runtimeOwner).ensureRuntimeOwner().Lifecycle.Cancel()
		return err
	}
	return nil
}
func (a *Frontend) StartInboundGC() {
	runAsync(&a.runtimeOwner.Lifecycle, a.asyncRunner, func() { a.runtimeOwner.InboundDeduper.RunGC(a.Context()) })
}
func (a *Frontend) ResetStartupState() error { return a.bindings.StartupRecovery.ResetStartupState() }
func (a *Frontend) RecoverFrontend() error {
	return a.bindings.StartupRecovery.RecoverFrontendRuntimeState()
}
func (a *Frontend) Serve() error { return startFrontend(a.feishu, a.Context()) }
func (a *Frontend) StartBackground() {
	maintenance.StartPeriodic(a.Context(), func(fn func()) { runAsync(&a.runtimeOwner.Lifecycle, a.asyncRunner, fn) }, 24*time.Hour, a.bindings.MaintenanceCommands.RunDriveArtifactGC)
	maintenance.StartPeriodic(a.Context(), func(fn func()) { runAsync(&a.runtimeOwner.Lifecycle, a.asyncRunner, fn) }, 30*time.Second, a.bindings.MaintenanceCommands.CheckPendingUpgrades)
	scheduleStartupGroupAnnouncementRefreshes(a.runtimeOwner.Announcements, a.bindings.AnnouncementQuery)
	startupRecovery := a.bindings.StartupRecovery
	runAsync(&a.runtimeOwner.Lifecycle, a.asyncRunner, func() { startupRecovery.SendStartupReadyNotifications() })
	healInputs := FeishuAppConfigHealInputs{
		Client: a.feishu, Config: a.cfg, ConfigMu: a.ConfigMu(), ConfigIndex: a.frontendConfigIndex,
		FrontendID: a.frontendID, State: a.stateView, Context: a.Context,
		Notifications: a.bindings.Notifications,
	}
	runAsync(&a.runtimeOwner.Lifecycle, a.asyncRunner, func() { runFeishuAppConfigHealWith(healInputs) })
}
