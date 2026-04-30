package app

import (
	"time"

	appupgradecmd "feidex/internal/app/upgradecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

var upgradeDisplayLocation = appupgradecmd.DisplayLocation

type appUpgradeService struct {
	inner appupgradecmd.UpgradeService
}

func newAppUpgradeService(app *App) appUpgradeService {
	return appUpgradeService{inner: newUpgradeServiceInner(app)}
}

func (s appUpgradeService) renderUpgradePreparingCard(sessionKey string) map[string]any {
	return s.inner.RenderUpgradePreparingCard(sessionKey)
}

func (s appUpgradeService) renderUpgradeFailedCard(sessionKey, errText string) map[string]any {
	return s.inner.RenderUpgradeFailedCard(sessionKey, errText)
}

func (s appUpgradeService) renderUpgradeCardForVersion(sessionKey, ownerUserID, requestedVersion string) (map[string]any, error) {
	return s.inner.RenderUpgradeCardForVersion(sessionKey, ownerUserID, requestedVersion)
}

func (s appUpgradeService) renderUpgradeDevCard(sessionKey, ownerUserID string) (map[string]any, error) {
	return s.inner.RenderUpgradeDevCard(sessionKey, ownerUserID)
}

func (s appUpgradeService) renderUpgradeCardForTarget(sessionKey, ownerUserID, requestedVersion string, useDevRelease bool) (map[string]any, error) {
	return s.inner.RenderUpgradeCardForTarget(sessionKey, ownerUserID, requestedVersion, useDevRelease)
}

func (s appUpgradeService) commandUpgrade(msg *feishu.InboundMessage, args []string) error {
	return s.inner.CommandUpgrade(msg, args)
}

func (s appUpgradeService) replyUpgradeCard(msg *feishu.InboundMessage, targetVersion string) error {
	return s.inner.ReplyUpgradeCard(msg, targetVersion)
}

func (s appUpgradeService) replyUpgradeDevCard(msg *feishu.InboundMessage) error {
	return s.inner.ReplyUpgradeDevCard(msg)
}

func (s appUpgradeService) completeUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteUpgradeAction(action, actionName)
}

func (s appUpgradeService) validateUpgradeRuntime() (string, string, error) {
	return s.inner.ValidateUpgradeRuntime()
}

func (s appUpgradeService) renderUpgradeConfirmCard(title, sessionKey, requestID string, payload upgradePendingPayload, lines []string) map[string]any {
	return s.inner.RenderUpgradeConfirmCard(title, sessionKey, requestID, payload, lines)
}

func (s appUpgradeService) commandUpgradeLocalPick(msg *feishu.InboundMessage) error {
	return s.inner.CommandUpgradeLocalPick(msg)
}

func (s appUpgradeService) commandUpgradeLocalPath(msg *feishu.InboundMessage, rawPath string) error {
	return s.inner.CommandUpgradeLocalPath(msg, rawPath)
}

func (s appUpgradeService) createUpgradeLocalPickerRequest(sessionKey string, ws *config.Workspace, ownerUserID, feishuMsgID string) (string, pathPickerPayload, error) {
	return s.inner.CreateUpgradeLocalPickerRequest(sessionKey, ws, ownerUserID, feishuMsgID)
}

func (s appUpgradeService) createLocalUpgradeRequest(sessionKey, ownerUserID, feishuMsgID, selectedPath string) (string, upgradePendingPayload, error) {
	return s.inner.CreateLocalUpgradeRequest(sessionKey, ownerUserID, feishuMsgID, selectedPath)
}

func (s appUpgradeService) completeUpgradeLocalPick(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteUpgradeLocalPick(action)
}

func (s appUpgradeService) completeUpgradeLocalBinaryConfirm(action *feishu.CardAction, pending *state.PendingRequest, payload pathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteUpgradeLocalBinaryConfirm(action, pending, payload, selectedPath)
}

func (s appUpgradeService) stageLocalUpgradeArtifact(requestID, sourcePath string) (string, string, int64, error) {
	return s.inner.StageLocalUpgradeArtifact(requestID, sourcePath)
}

func resolveUpgradeLocalSourcePath(ws *config.Workspace, rawPath string) (string, error) {
	return appupgradecmd.ResolveUpgradeLocalSourcePath(ws, rawPath)
}

func formatUpgradeReleasePublishedAt(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.In(upgradeDisplayLocation).Format("2006-01-02 15:04:05")
}

func upgradeLocalConfirmLines(svc appupgradecmd.UpgradeService, binaryPath string) []string {
	return svc.UpgradeLocalConfirmLines(binaryPath)
}
