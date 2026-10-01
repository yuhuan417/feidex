package app

import (
	appcommandmatch "feidex/internal/app/commandmatch"

	"strings"

	appupgradecmd "feidex/internal/app/upgradecmd"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
)

func newUpgradeService(app *App) appupgradecmd.UpgradeService {
	return serviceFor(app, "upgradeService", func() appupgradecmd.UpgradeService {
		deps := appupgradecmd.UpgradeServiceDeps{
			CurrentVersion: func() string { return currentVersion() },
			CurrentGOOS:    func() string { return currentGOOS() },
			CurrentGOARCH:  func() string { return currentGOARCH() },
			NewReleaseClient: func() appupgradecmd.ReleaseClient {
				return newReleaseClient()
			},
			NewDaemonManager: func(serviceName string) (daemon.Manager, error) {
				return newDaemonManager(serviceName)
			},
			StartDaemonUpgrade: func(spec daemon.UpgradeSpec) (string, error) {
				return startDaemonUpgrade(spec)
			},
			NormalizeUpgradeVersion: func(raw string) (string, error) {
				return appcommandmatch.NormalizeUpgradeVersion(raw)
			},
			RenderSystemMenuCard: func(sessionKey string) map[string]any {
				return renderSystemMenuCard(app, sessionKey)
			},
		}

		adapter := &appupgradecmd.DefaultApp{
			ContextFunc: app.Context,
			FeishuClientFunc: func() appupgradecmd.FeishuClient {
				return app.feishu
			},
			StateFunc: func() appupgradecmd.UpgradeState {
				return app.State()
			},
			CurrentWorkspaceFunc: func(msg *feishu.InboundMessage) (string, *config.Workspace) {
				sessionKey, _, ws := currentWorkspaceForMessage(app, msg)
				return sessionKey, ws
			},
			WorkspaceForSessionFunc: func(sessionKey string) *config.Workspace {
				wsID := defaultWorkspaceID(app)
				if sess := app.State().Session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
					wsID = sess.WorkspaceID
				}
				return config.FindWorkspace(app.cfg, wsID)
			},
			RenderPathPickerCardFunc: func(requestID string, payload appupgradecmd.PathPickerPayload) (map[string]any, error) {
				return newWorkspaceRenderService(app).RenderPathPickerCard(requestID, payload)
			},
			DataDirFunc: func() string {
				return app.cfg.DataDir
			},
			DaemonNameFunc: func() string {
				app.configMutex().RLock()
				defer app.configMutex().RUnlock()
				return strings.TrimSpace(app.cfg.Daemon.ServiceName)
			},
			MakeSessionKeyFunc: func(msg *feishu.InboundMessage) string {
				return makeSessionKey(app, msg)
			},
			ReplyInThreadFunc: func(chatType string) bool {
				return replyInThreadEnabled(app, chatType)
			},
			MenuCardBodyFunc: func(action, body string) string {
				return menuCardBody(action, body)
			},
		}
		return appupgradecmd.NewUpgradeService(adapter, deps)
	})
}
