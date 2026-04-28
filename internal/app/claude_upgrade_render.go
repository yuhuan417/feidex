package app

import (
	"time"

	"feidex/internal/app/upgraderender"
	"feidex/internal/state"
)

func (s upgradeRenderService) renderClaudeUpgradeStatusCard(sessionKey string, view claudeUpgradeView, latestChecked bool) map[string]any {
	return upgraderender.RenderClaudeUpgradeStatusCard(s.app.feishu, sessionKey, claudeViewToUpgraderender(view), latestChecked)
}

func (s upgradeRenderService) prepareClaudeUpgradeCard(sessionKey, ownerUserID string, view claudeUpgradeView) (map[string]any, string, error) {
	uv := claudeViewToUpgraderender(view)
	if view.Snapshot.Running || !view.Probe.Supported || view.BusyReason != "" || view.LatestError != "" || view.LatestVersion == "" || view.Probe.CurrentVersion == view.LatestVersion {
		return upgraderender.RenderClaudeUpgradeStatusCard(s.app.feishu, sessionKey, uv, true), "", nil
	}
	requestID, err := s.app.State().NextLocalID("claude-upgrade")
	if err != nil {
		return nil, "", err
	}
	payload := claudeUpgradePendingPayload{
		CurrentVersion: view.Probe.CurrentVersion,
		TargetVersion:  view.LatestVersion,
		Command:        view.Probe.Command,
		CommandPath:    view.Probe.CommandPath,
		NPMPath:        view.Probe.NPMPath,
	}
	if err := s.app.State().SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        claudeUpgradePendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		PayloadJSON: mustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(15 * time.Minute).Unix(),
	}); err != nil {
		return nil, "", err
	}
	return upgraderender.RenderClaudeUpgradeConfirmCard(s.app.feishu, sessionKey, requestID, payload.CurrentVersion, payload.TargetVersion), requestID, nil
}

func (s upgradeRenderService) renderClaudeUpgradeConfirmCard(sessionKey, requestID string, payload claudeUpgradePendingPayload) map[string]any {
	return upgraderender.RenderClaudeUpgradeConfirmCard(s.app.feishu, sessionKey, requestID, payload.CurrentVersion, payload.TargetVersion)
}

func (s upgradeRenderService) renderClaudeUpgradePreparingCard(sessionKey, body string) map[string]any {
	return upgraderender.RenderClaudeUpgradePreparingCard(s.app.feishu, body)
}

func (s upgradeRenderService) renderClaudeUpgradeFailedCard(sessionKey, errText string) map[string]any {
	return upgraderender.RenderClaudeUpgradeFailedCard(s.app.feishu, sessionKey, errText)
}

func (s upgradeRenderService) renderClaudeUpgradeOperationCard(sessionKey string, snapshot backendUpgradeSnapshot) map[string]any {
	return upgraderender.RenderClaudeUpgradeOperationCard(s.app.feishu, sessionKey, snapshot)
}

func (s upgradeRenderService) renderClaudeRestartOperationCard(sessionKey string, snapshot backendRestartSnapshot) map[string]any {
	return upgraderender.RenderClaudeRestartOperationCard(s.app.feishu, sessionKey, snapshot)
}

func claudeViewToUpgraderender(view claudeUpgradeView) upgraderender.ClaudeUpgradeView {
	return upgraderender.ClaudeUpgradeView{
		Probe:         view.Probe,
		LatestVersion: view.LatestVersion,
		LatestError:   view.LatestError,
		BusyReason:    view.BusyReason,
		Snapshot:      view.Snapshot,
		Restart:       view.Restart,
	}
}
