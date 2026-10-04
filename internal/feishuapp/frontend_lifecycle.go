package feishuapp

import (
	"context"
	"feidex/internal/runtime/maintenance"
	"time"
)

func (a *App) Prepare(ctx context.Context) error {
	a.beginLifecycle(ctx)
	if err := startMCPService(a, a.Context()); err != nil {
		a.runtimeView().ensureRuntimeOwner().Lifecycle.Cancel()
		return err
	}
	if err := startBackend(a, a.Context()); err != nil {
		a.runtimeView().ensureRuntimeOwner().Lifecycle.Cancel()
		return err
	}
	return nil
}
func (a *App) StartInboundGC() {
	runAsync(a, func() { a.runtimeOwner.InboundDeduper.RunGC(a.Context()) })
}
func (a *App) ResetStartupState() error { return a.bindings.StartupRecovery.ResetStartupState() }
func (a *App) RecoverFrontend() error {
	return a.bindings.StartupRecovery.RecoverFrontendRuntimeState()
}
func (a *App) Serve() error { return startFrontend(a, a.Context()) }
func (a *App) StartBackground() {
	maintenance.StartPeriodic(a.Context(), func(fn func()) { runAsync(a, fn) }, 24*time.Hour, a.bindings.MaintenanceCommands.RunDriveArtifactGC)
	maintenance.StartPeriodic(a.Context(), func(fn func()) { runAsync(a, fn) }, 30*time.Second, a.bindings.MaintenanceCommands.CheckPendingUpgrades)
	scheduleStartupGroupAnnouncementRefreshes(a.runtimeOwner.Announcements, a.bindings.AnnouncementQuery)
	runAsync(a, func() { sendStartupReadyNotifications(a) })
	runAsync(a, func() { runFeishuAppConfigHeal(a) })
}
