package app

import (
	"strings"
	"time"

	appbackend "feidex/internal/app/backend"
	"feidex/internal/app/upgraderender"
	appruntime "feidex/internal/runtime"
	"feidex/internal/state"
)

// upgradeTargetMatchesCurrent reports whether targetVersion names the version
// already installed. "latest" and empty never match.
func upgradeTargetMatchesCurrent(currentVersion, targetVersion string) bool {
	targetVersion = strings.TrimSpace(targetVersion)
	if targetVersion == "" || strings.EqualFold(targetVersion, "latest") {
		return false
	}
	return strings.TrimSpace(currentVersion) == targetVersion
}

// upgradeRenderService renders upgrade cards for either backend; the spec
// selects which one.
type upgradeRenderService struct {
	app *App
}

func newUpgradeRenderService(app *App) upgradeRenderService {
	return upgradeRenderService{app: app}
}

func (s upgradeRenderService) renderUpgradeStatusCard(spec upgraderender.Spec, sessionKey string, view backendUpgradeView, latestChecked bool) map[string]any {
	return upgraderender.RenderUpgradeStatusCard(spec, s.app.feishu, sessionKey, view, latestChecked)
}

// prepareUpgradeCard renders the confirmation card and persists the pending
// request, or returns the status card when the upgrade cannot start.
func (s upgradeRenderService) prepareUpgradeCard(spec upgraderender.Spec, pendingKind, idPrefix string, sessionKey, ownerUserID string, view backendUpgradeView) (map[string]any, string, error) {
	if view.Snapshot.Running || !view.Probe.Supported || view.BusyReason != "" || view.LatestError != "" || view.LatestVersion == "" || upgradeTargetMatchesCurrent(view.Probe.CurrentVersion, view.LatestVersion) {
		return upgraderender.RenderUpgradeStatusCard(spec, s.app.feishu, sessionKey, view, true), "", nil
	}
	requestID, err := s.app.State().NextLocalID(idPrefix)
	if err != nil {
		return nil, "", err
	}
	payload := appruntime.BackendUpgradePendingPayload{
		CurrentVersion: view.Probe.CurrentVersion,
		TargetVersion:  view.LatestVersion,
		Command:        view.Probe.Command,
		CommandPath:    view.Probe.CommandPath,
		UpdateCommand:  view.Probe.UpdateCommand,
	}
	if err := s.app.State().SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        pendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		PayloadJSON: mustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(15 * time.Minute).Unix(),
	}); err != nil {
		return nil, "", err
	}
	return upgraderender.RenderUpgradeConfirmCard(spec, s.app.feishu, sessionKey, requestID, payload.CurrentVersion, payload.TargetVersion, payload.UpdateCommand), requestID, nil
}

func (s upgradeRenderService) renderUpgradePreparingCard(spec upgraderender.Spec, sessionKey, body string) map[string]any {
	return upgraderender.RenderUpgradePreparingCard(spec, s.app.feishu, body)
}

func (s upgradeRenderService) renderUpgradeFailedCard(spec upgraderender.Spec, sessionKey, errText string) map[string]any {
	return upgraderender.RenderUpgradeFailedCard(spec, s.app.feishu, sessionKey, errText)
}

func (s upgradeRenderService) renderUpgradeOperationCard(spec upgraderender.Spec, sessionKey string, snapshot appbackend.BackendUpgradeSnapshot) map[string]any {
	return upgraderender.RenderUpgradeOperationCard(spec, s.app.feishu, sessionKey, snapshot)
}

func (s upgradeRenderService) renderRestartOperationCard(spec upgraderender.Spec, sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
	return upgraderender.RenderRestartOperationCard(spec, s.app.feishu, sessionKey, snapshot)
}
