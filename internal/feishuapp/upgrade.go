package feishuapp

import (
	appfeatures "feidex/internal/application/features"

	"strings"

	appupgradecmd "feidex/internal/adapter/feishu/upgradecmd"
	"feidex/internal/adapter/feishu/workspacecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
)

func BuildUpgrades(app *App, workspaceConfiguration *workspacecmd.ConfigService) appupgradecmd.UpgradeService {
	outboundFrontend := app.FrontendID()
	runtimeOwner := app.runtimeOwner
	workspacePresentation := app.bindings.WorkspacePresentation

	deps := appupgradecmd.UpgradeServiceDeps{
		CurrentVersion: func() string { return currentVersion() },
		CurrentGOARCH:  func() string { return currentGOARCH() },
		NormalizeUpgradeVersion: func(raw string) (string, error) {
			return appfeatures.NormalizeUpgradeVersion(raw)
		},
		RenderSystemMenuCard: func(sessionKey string) map[string]any {
			spec, _ := menuGroupSpec("menu.group.system")
			return renderSystemMenuCardData(app.configView().configuredBackend(), planModeTitleForSession(app.State(), app != nil, sessionKey, spec.Label), app.feishu, sessionKey)
		},
	}

	adapter := &appupgradecmd.DefaultApp{
		ContextFunc: app.Context,
		OutboundFunc: func() appupgradecmd.Outbound {
			return newEffectOutbound(outboundFrontend, newEffectRunner(runtimeOwner))
		},
		CardRendererFunc: func() appupgradecmd.CardRenderer { return simpleStatusCardRenderer{client: app.feishu} },
		StateFunc: func() appupgradecmd.UpgradeState {
			return app.State()
		},
		CurrentWorkspaceFunc: func(msg *feishu.InboundMessage) (string, *config.Workspace) {
			sessionKey, _, ws := currentWorkspaceForMessage(workspaceConfiguration, msg)
			return sessionKey, ws
		},
		WorkspaceForSessionFunc: func(sessionKey string) *config.Workspace {
			wsID := app.configView().defaultWorkspaceID()
			if sess := app.State().Session(sessionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
				wsID = sess.WorkspaceID
			}
			return config.FindWorkspace(app.cfg, wsID)
		},
		RenderPathPickerCardFunc: func(requestID string, payload appupgradecmd.PathPickerPayload) (map[string]any, error) {
			return workspacePresentation.RenderPathPickerCard(requestID, payload)
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
			return app.configView().makeSessionKey(msg)
		},
		ReplyInThreadFunc: func(chatType string) bool {
			return app.configView().replyInThreadEnabled()
		},
		MenuCardBodyFunc: func(action, body string) string {
			return menuCardBody(action, body)
		},
	}
	return appupgradecmd.NewUpgradeService(adapter, deps, app.bindings.UpgradeWorkflow)
}
