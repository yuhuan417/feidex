package app

import (
	"time"

	"feidex/internal/app/upgraderender"
	"feidex/internal/state"
)

type upgradeRenderService struct {
	app *App
}
func newUpgradeRenderService(app *App) upgradeRenderService {
	return upgradeRenderService{app: app}
}

func (s upgradeRenderService) renderCodexUpgradeStatusCard(sessionKey string, view codexUpgradeView, latestChecked bool) map[string]any {
	return upgraderender.RenderCodexUpgradeStatusCard(s.app.feishu, sessionKey, codexViewToUpgraderender(view), latestChecked)
}

func (s upgradeRenderService) prepareCodexUpgradeCard(sessionKey, ownerUserID string, view codexUpgradeView) (map[string]any, string, error) {
	uv := codexViewToUpgraderender(view)
	if view.Snapshot.Running || !view.Probe.Supported || view.BusyReason != "" || view.LatestError != "" || view.LatestVersion == "" || view.Probe.CurrentVersion == view.LatestVersion {
		return upgraderender.RenderCodexUpgradeStatusCard(s.app.feishu, sessionKey, uv, true), "", nil
	}
	requestID, err := appState(s.app).nextLocalID("codex-upgrade")
	if err != nil {
		return nil, "", err
	}
	payload := codexUpgradePendingPayload{
		CurrentVersion: view.Probe.CurrentVersion,
		TargetVersion:  view.LatestVersion,
		Command:        view.Probe.Command,
		CommandPath:    view.Probe.CommandPath,
		NPMPath:        view.Probe.NPMPath,
	}
	if err := appState(s.app).savePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        codexUpgradePendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		PayloadJSON: mustJSON(payload),
		Status:      "pending",
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(15 * time.Minute).Unix(),
	}); err != nil {
		return nil, "", err
	}
	return upgraderender.RenderCodexUpgradeConfirmCard(s.app.feishu, sessionKey, requestID, payload.CurrentVersion, payload.TargetVersion), requestID, nil
}

func (s upgradeRenderService) renderCodexUpgradeConfirmCard(sessionKey, requestID string, payload codexUpgradePendingPayload) map[string]any {
	return upgraderender.RenderCodexUpgradeConfirmCard(s.app.feishu, sessionKey, requestID, payload.CurrentVersion, payload.TargetVersion)
}

func (s upgradeRenderService) renderCodexUpgradePreparingCard(sessionKey, body string) map[string]any {
	return upgraderender.RenderCodexUpgradePreparingCard(s.app.feishu, body)
}

func (s upgradeRenderService) renderCodexUpgradeFailedCard(sessionKey, errText string) map[string]any {
	return upgraderender.RenderCodexUpgradeFailedCard(s.app.feishu, sessionKey, errText)
}

func (s upgradeRenderService) renderCodexUpgradeOperationCard(sessionKey string, snapshot backendUpgradeSnapshot) map[string]any {
	return upgraderender.RenderCodexUpgradeOperationCard(s.app.feishu, sessionKey, snapshot)
}

func (s upgradeRenderService) renderCodexRestartOperationCard(sessionKey string, snapshot backendRestartSnapshot) map[string]any {
	return upgraderender.RenderCodexRestartOperationCard(s.app.feishu, sessionKey, snapshot)
}

func codexViewToUpgraderender(view codexUpgradeView) upgraderender.CodexUpgradeView {
	return upgraderender.CodexUpgradeView{
		Probe:         view.Probe,
		LatestVersion: view.LatestVersion,
		LatestError:   view.LatestError,
		BusyReason:    view.BusyReason,
		Snapshot:      view.Snapshot,
		Restart:       view.Restart,
	}
}
