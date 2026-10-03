package app

import (
	"context"
	appfeatures "feidex/internal/application/features"

	"strings"

	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"
)

type upgradeOutbound struct{ app *App }

func (o upgradeOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}

type upgradeCardRenderer struct{ app *App }

func (r upgradeCardRenderer) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	if r.app == nil || r.app.feishu == nil {
		return nil
	}
	return r.app.feishu.SimpleStatusCard(title, color, body, buttons)
}

func newUpgradeService(app *App) appupgradecmd.UpgradeService {

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
			return appfeatures.NormalizeUpgradeVersion(raw)
		},
		RenderSystemMenuCard: func(sessionKey string) map[string]any {
			return renderSystemMenuCard(app, sessionKey)
		},
	}

	adapter := &appupgradecmd.DefaultApp{
		ContextFunc:      app.Context,
		OutboundFunc:     func() appupgradecmd.Outbound { return upgradeOutbound{app: app} },
		CardRendererFunc: func() appupgradecmd.CardRenderer { return upgradeCardRenderer{app: app} },
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
}
