package app

import (
	appbackend "feidex/internal/app/backend"
	appruntime "feidex/internal/app/runtime"

	"strings"
	"time"

	"feidex/internal/app/upgraderender"
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

type upgradeRenderService struct {
	app *App
}

func newUpgradeRenderService(app *App) upgradeRenderService {
	return upgradeRenderService{app: app}
}

func (s upgradeRenderService) renderCodexUpgradeStatusCard(sessionKey string, view backendUpgradeView, latestChecked bool) map[string]any {
	return upgraderender.RenderUpgradeStatusCard(upgraderender.CodexSpec, s.app.feishu, sessionKey, view, latestChecked)
}

func (s upgradeRenderService) prepareCodexUpgradeCard(sessionKey, ownerUserID string, view backendUpgradeView) (map[string]any, string, error) {
	uv := view
	if view.Snapshot.Running || !view.Probe.Supported || view.BusyReason != "" || view.LatestError != "" || view.LatestVersion == "" || upgradeTargetMatchesCurrent(view.Probe.CurrentVersion, view.LatestVersion) {
		return upgraderender.RenderUpgradeStatusCard(upgraderender.CodexSpec, s.app.feishu, sessionKey, uv, true), "", nil
	}
	requestID, err := s.app.State().NextLocalID("codex-upgrade")
	if err != nil {
		return nil, "", err
	}
	payload := appruntime.CodexUpgradePendingPayload{
		CurrentVersion: view.Probe.CurrentVersion,
		TargetVersion:  view.LatestVersion,
		Command:        view.Probe.Command,
		CommandPath:    view.Probe.CommandPath,
		UpdateCommand:  view.Probe.UpdateCommand,
	}
	if err := s.app.State().SavePending(&state.PendingRequest{
		ID:          requestID,
		Kind:        codexUpgradePendingKind,
		SessionKey:  sessionKey,
		OwnerUserID: ownerUserID,
		PayloadJSON: mustJSON(payload),
		Status:      state.PendingRequestStatusPending.String(),
		CreatedAt:   time.Now().Unix(),
		ExpiresAt:   time.Now().Add(15 * time.Minute).Unix(),
	}); err != nil {
		return nil, "", err
	}
	return upgraderender.RenderUpgradeConfirmCard(upgraderender.CodexSpec, s.app.feishu, sessionKey, requestID, payload.CurrentVersion, payload.TargetVersion, payload.UpdateCommand), requestID, nil
}

func (s upgradeRenderService) renderCodexUpgradePreparingCard(sessionKey, body string) map[string]any {
	return upgraderender.RenderUpgradePreparingCard(upgraderender.CodexSpec, s.app.feishu, body)
}

func (s upgradeRenderService) renderCodexUpgradeFailedCard(sessionKey, errText string) map[string]any {
	return upgraderender.RenderUpgradeFailedCard(upgraderender.CodexSpec, s.app.feishu, sessionKey, errText)
}

func (s upgradeRenderService) renderCodexUpgradeOperationCard(sessionKey string, snapshot appbackend.BackendUpgradeSnapshot) map[string]any {
	return upgraderender.RenderUpgradeOperationCard(upgraderender.CodexSpec, s.app.feishu, sessionKey, snapshot)
}

func (s upgradeRenderService) renderCodexRestartOperationCard(sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
	return upgraderender.RenderRestartOperationCard(upgraderender.CodexSpec, s.app.feishu, sessionKey, snapshot)
}

func (s upgradeRenderService) renderClaudeUpgradeStatusCard(sessionKey string, view backendUpgradeView, latestChecked bool) map[string]any {
	return upgraderender.RenderUpgradeStatusCard(upgraderender.ClaudeSpec, s.app.feishu, sessionKey, view, latestChecked)
}

func (s upgradeRenderService) prepareClaudeUpgradeCard(sessionKey, ownerUserID string, view backendUpgradeView) (map[string]any, string, error) {
	uv := view
	if view.Snapshot.Running || !view.Probe.Supported || view.BusyReason != "" || view.LatestError != "" || view.LatestVersion == "" || upgradeTargetMatchesCurrent(view.Probe.CurrentVersion, view.LatestVersion) {
		return upgraderender.RenderUpgradeStatusCard(upgraderender.ClaudeSpec, s.app.feishu, sessionKey, uv, true), "", nil
	}
	requestID, err := s.app.State().NextLocalID("claude-upgrade")
	if err != nil {
		return nil, "", err
	}
	payload := appruntime.ClaudeUpgradePendingPayload{
		CurrentVersion: view.Probe.CurrentVersion,
		TargetVersion:  view.LatestVersion,
		Command:        view.Probe.Command,
		CommandPath:    view.Probe.CommandPath,
		UpdateCommand:  view.Probe.UpdateCommand,
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
	return upgraderender.RenderUpgradeConfirmCard(upgraderender.ClaudeSpec, s.app.feishu, sessionKey, requestID, payload.CurrentVersion, payload.TargetVersion, payload.UpdateCommand), requestID, nil
}

func (s upgradeRenderService) renderClaudeUpgradePreparingCard(sessionKey, body string) map[string]any {
	return upgraderender.RenderUpgradePreparingCard(upgraderender.ClaudeSpec, s.app.feishu, body)
}

func (s upgradeRenderService) renderClaudeUpgradeFailedCard(sessionKey, errText string) map[string]any {
	return upgraderender.RenderUpgradeFailedCard(upgraderender.ClaudeSpec, s.app.feishu, sessionKey, errText)
}

func (s upgradeRenderService) renderClaudeUpgradeOperationCard(sessionKey string, snapshot appbackend.BackendUpgradeSnapshot) map[string]any {
	return upgraderender.RenderUpgradeOperationCard(upgraderender.ClaudeSpec, s.app.feishu, sessionKey, snapshot)
}

func (s upgradeRenderService) renderClaudeRestartOperationCard(sessionKey string, snapshot appbackend.BackendRestartSnapshot) map[string]any {
	return upgraderender.RenderRestartOperationCard(upgraderender.ClaudeSpec, s.app.feishu, sessionKey, snapshot)
}
