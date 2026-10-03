package feishuapp

import "context"

func (a *App) Prepare(ctx context.Context) error {
	a.beginLifecycle(ctx)
	if err := startMCPService(a, a.Context()); err != nil {
		a.frontendRuntime.Cancel()
		return err
	}
	if err := startBackend(a, a.Context()); err != nil {
		a.frontendRuntime.Cancel()
		return err
	}
	return nil
}
func (a *App) StartInboundGC()  { runAsync(a, func() { a.deduper.RunGC(a.Context()) }) }
func (a *App) RecoverShared()   { recoverSharedRuntimeState(a) }
func (a *App) RecoverFrontend() { recoverFrontendRuntimeState(a) }
func (a *App) Serve() error     { return startFrontend(a, a.Context()) }
func (a *App) StartBackground() {
	newRuntimeMaintenanceService(a).StartDriveArtifactGCLoop(a.Context())
	newRuntimeMaintenanceService(a).StartUpgradeCheckLoop(a.Context())
	scheduleStartupGroupAnnouncementRefreshes(a)
	go sendStartupReadyNotifications(a)
	runAsync(a, func() { runFeishuAppConfigHeal(a) })
}
