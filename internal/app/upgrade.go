package app

import (
	"strings"

	appupgradecmd "feidex/internal/app/upgradecmd"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
)

type upgradePendingPayload = appupgradecmd.UpgradePendingPayload

const upgradeLocalBinaryPendingKind = appupgradecmd.UpgradeLocalBinaryPendingKind

func newUpgradeServiceInner(app *App) appupgradecmd.UpgradeService {
	appupgradecmd.CurrentVersion = func() string { return currentVersion() }
	appupgradecmd.CurrentGOOS = func() string { return currentGOOS() }
	appupgradecmd.CurrentGOARCH = func() string { return currentGOARCH() }
	appupgradecmd.NewReleaseClient = func() appupgradecmd.ReleaseClient {
		return newReleaseClient()
	}
	appupgradecmd.NewDaemonManager = func(serviceName string) (daemon.Manager, error) {
		return newDaemonManager(serviceName)
	}
	appupgradecmd.StartDaemonUpgrade = func(spec daemon.UpgradeSpec) (string, error) {
		return startDaemonUpgrade(spec)
	}
	appupgradecmd.NormalizeUpgradeVersion = func(raw string) (string, error) {
		return normalizeUpgradeVersion(raw)
	}
	appupgradecmd.RenderSystemMenuCard = func(sessionKey string) map[string]any {
		return renderSystemMenuCard(app, sessionKey)
	}

	adapter := &appupgradecmd.DefaultApp{
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
			return newWorkspaceRenderServiceInner(app).RenderPathPickerCard(requestID, payload)
		},
		DataDirFunc: func() string {
			return app.cfg.DataDir
		},
		DaemonNameFunc: func() string {
			app.configMu.RLock()
			defer app.configMu.RUnlock()
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
	return appupgradecmd.NewUpgradeService(adapter)
}
