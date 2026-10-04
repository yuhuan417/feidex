package feishuapp

import (
	"feidex/internal/application/backendmaintenance"
	"strings"

	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/upgraderender"
)

type upgradeRenderService struct {
	backendMaintenance map[string]*backendmaintenance.Service
	renderer           upgraderender.StatusCardRenderer
}

// upgradeTargetMatchesCurrent reports whether targetVersion names the version
// already installed. "latest" and empty never match.

func BuildUpgradePresentation(renderer upgraderender.StatusCardRenderer, backendMaintenance map[string]*backendmaintenance.Service) upgradeRenderService {
	return upgradeRenderService{backendMaintenance: backendMaintenance, renderer: renderer}
}

func (s upgradeRenderService) renderUpgradeStatusCard(spec upgraderender.Spec, sessionKey string, view upgraderender.UpgradeView, latestChecked bool) map[string]any {
	return upgraderender.RenderUpgradeStatusCard(spec, s.renderer, sessionKey, view, latestChecked)
}

// prepareUpgradeCard renders the confirmation card and persists the pending
// request, or returns the status card when the upgrade cannot start.
func (s upgradeRenderService) prepareUpgradeCard(spec upgraderender.Spec, pendingKind, idPrefix string, sessionKey, ownerUserID string, view upgraderender.UpgradeView) (map[string]any, string, error) {
	kind := strings.TrimSuffix(pendingKind, "_self_upgrade")
	confirmation, err := s.backendMaintenance[kind].Prepare(sessionKey, ownerUserID, backendmaintenance.View{Probe: view.Probe, BusyReason: view.BusyReason, Snapshot: view.Snapshot, Restart: view.Restart, LatestVersion: view.LatestVersion, LatestError: view.LatestError})
	if err != nil {
		return nil, "", err
	}
	if confirmation.RequestID == "" {
		return upgraderender.RenderUpgradeStatusCard(spec, s.renderer, sessionKey, view, true), "", nil
	}
	payload := confirmation.Payload
	return upgraderender.RenderUpgradeConfirmCard(spec, s.renderer, sessionKey, confirmation.RequestID, payload.CurrentVersion, payload.TargetVersion, payload.UpdateCommand), confirmation.RequestID, nil
}

func (s upgradeRenderService) renderUpgradePreparingCard(spec upgraderender.Spec, sessionKey, body string) map[string]any {
	return upgraderender.RenderUpgradePreparingCard(spec, s.renderer, body)
}

func (s upgradeRenderService) renderUpgradeFailedCard(spec upgraderender.Spec, sessionKey, errText string) map[string]any {
	return upgraderender.RenderUpgradeFailedCard(spec, s.renderer, sessionKey, errText)
}

func (s upgradeRenderService) renderUpgradeOperationCard(spec upgraderender.Spec, sessionKey string, snapshot appbackend.BackendUpgradeSnapshot) map[string]any {
	return upgraderender.RenderUpgradeOperationCard(spec, s.renderer, sessionKey, snapshot)
}

func (s upgradeRenderService) renderRestartOperationCard(spec upgraderender.Spec, sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
	return upgraderender.RenderRestartOperationCard(spec, s.renderer, sessionKey, snapshot)
}
