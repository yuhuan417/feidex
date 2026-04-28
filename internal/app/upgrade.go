package app

import (
	"strings"
	"time"

	appupgradecmd "feidex/internal/app/upgradecmd"
	"feidex/internal/config"
	"feidex/internal/daemon"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Type and const aliases — keep existing symbols for callers in this package.
// ---------------------------------------------------------------------------

type upgradePendingPayload = appupgradecmd.UpgradePendingPayload

const upgradeLocalBinaryPendingKind = appupgradecmd.UpgradeLocalBinaryPendingKind

// upgradeDisplayLocation is the timezone used when formatting upgrade
// timestamps. Tests may override this value. The delegated upgradecmd code
// uses upgradecmd.DisplayLocation instead.
var upgradeDisplayLocation = time.Local

// ---------------------------------------------------------------------------
// appUpgradeService — thin wrapper around upgradecmd.UpgradeService
// ---------------------------------------------------------------------------

// appUpgradeService wraps upgradecmd.UpgradeService. The embedded service
// carries all business logic; the app field is retained for methods defined
// in upgrade_local.go that access the App directly.
type appUpgradeService struct {
	appupgradecmd.UpgradeService
	app *App
}

// newAppUpgradeService creates an appUpgradeService bound to the given App.
// It wires upgradecmd package-level function variables and constructs a
// DefaultApp adapter that satisfies the upgradecmd.App interface.
func newAppUpgradeService(app *App) appUpgradeService {
	// Wire package-level function variables. The closures capture the app/
	// variables (not their current values), so test overrides of currentVersion
	// etc. take effect even when set before newAppUpgradeService is called.
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

	var svc appUpgradeService
	adapter := &appupgradecmd.DefaultApp{
		FeishuClientFunc: func() appupgradecmd.FeishuClient {
			return app.feishu
		},
		StateFunc: func() appupgradecmd.UpgradeState {
			return app.State()
		},
		CurrentWorkspaceFunc: func(msg *feishu.InboundMessage) (string, *config.Workspace) {
			sessionKey, _, ws := newWorkspaceConfigService(app).currentWorkspaceForMessage(msg)
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
			return newWorkspaceRenderService(app).renderPathPickerCard(requestID, payload)
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
	svc = appUpgradeService{
		UpgradeService: appupgradecmd.NewUpgradeService(adapter),
		app:            app,
	}
	return svc
}

// ---------------------------------------------------------------------------
// Delegation wrappers — preserve the unexported method surface expected by
// callers in this package (action registries, menu actions, tests, and
// upgrade_local.go).
// ---------------------------------------------------------------------------

func (s appUpgradeService) renderUpgradePreparingCard(sessionKey string) map[string]any {
	return s.UpgradeService.RenderUpgradePreparingCard(sessionKey)
}

func (s appUpgradeService) renderUpgradeFailedCard(sessionKey, errText string) map[string]any {
	return s.UpgradeService.RenderUpgradeFailedCard(sessionKey, errText)
}

func (s appUpgradeService) renderUpgradeCardForVersion(sessionKey, ownerUserID, requestedVersion string) (map[string]any, error) {
	return s.UpgradeService.RenderUpgradeCardForVersion(sessionKey, ownerUserID, requestedVersion)
}

func (s appUpgradeService) renderUpgradeDevCard(sessionKey, ownerUserID string) (map[string]any, error) {
	return s.UpgradeService.RenderUpgradeDevCard(sessionKey, ownerUserID)
}

func (s appUpgradeService) renderUpgradeCardForTarget(sessionKey, ownerUserID, requestedVersion string, useDevRelease bool) (map[string]any, error) {
	return s.UpgradeService.RenderUpgradeCardForTarget(sessionKey, ownerUserID, requestedVersion, useDevRelease)
}

func (s appUpgradeService) commandUpgrade(msg *feishu.InboundMessage, args []string) error {
	return s.UpgradeService.CommandUpgrade(msg, args)
}

func (s appUpgradeService) replyUpgradeCard(msg *feishu.InboundMessage, targetVersion string) error {
	return s.UpgradeService.ReplyUpgradeCard(msg, targetVersion)
}

func (s appUpgradeService) replyUpgradeDevCard(msg *feishu.InboundMessage) error {
	return s.UpgradeService.ReplyUpgradeDevCard(msg)
}

func (s appUpgradeService) completeUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	return s.UpgradeService.CompleteUpgradeAction(action, actionName)
}

func (s appUpgradeService) validateUpgradeRuntime() (string, string, error) {
	return s.UpgradeService.ValidateUpgradeRuntime()
}

func (s appUpgradeService) renderUpgradeConfirmCard(title, sessionKey, requestID string, payload upgradePendingPayload, lines []string) map[string]any {
	return s.UpgradeService.RenderUpgradeConfirmCard(title, sessionKey, requestID, payload, lines)
}

// formatUpgradeReleasePublishedAt is retained locally so that tests that
// override upgradeDisplayLocation still compile. The delegated code path in
// upgradecmd uses upgradecmd.DisplayLocation instead.
func formatUpgradeReleasePublishedAt(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.In(upgradeDisplayLocation).Format("2006-01-02 15:04:05")
}
