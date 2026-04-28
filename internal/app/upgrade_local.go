package app

import (
	appupgradecmd "feidex/internal/app/upgradecmd"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (s appUpgradeService) commandUpgradeLocalPick(msg *feishu.InboundMessage) error {
	return s.UpgradeService.CommandUpgradeLocalPick(msg)
}

func (s appUpgradeService) commandUpgradeLocalPath(msg *feishu.InboundMessage, rawPath string) error {
	return s.UpgradeService.CommandUpgradeLocalPath(msg, rawPath)
}

func resolveUpgradeLocalSourcePath(ws *config.Workspace, rawPath string) (string, error) {
	return appupgradecmd.ResolveUpgradeLocalSourcePath(ws, rawPath)
}

func (s appUpgradeService) createUpgradeLocalPickerRequest(sessionKey string, ws *config.Workspace, ownerUserID, feishuMsgID string) (string, pathPickerPayload, error) {
	return s.UpgradeService.CreateUpgradeLocalPickerRequest(sessionKey, ws, ownerUserID, feishuMsgID)
}

func (s appUpgradeService) createLocalUpgradeRequest(sessionKey, ownerUserID, feishuMsgID, selectedPath string) (string, upgradePendingPayload, error) {
	return s.UpgradeService.CreateLocalUpgradeRequest(sessionKey, ownerUserID, feishuMsgID, selectedPath)
}

func (s appUpgradeService) completeUpgradeLocalPick(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.UpgradeService.CompleteUpgradeLocalPick(action)
}

func (s appUpgradeService) completeUpgradeLocalBinaryConfirm(action *feishu.CardAction, pending *state.PendingRequest, payload pathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	return s.UpgradeService.CompleteUpgradeLocalBinaryConfirm(action, pending, payload, selectedPath)
}

func (s appUpgradeService) stageLocalUpgradeArtifact(requestID, sourcePath string) (string, string, int64, error) {
	return s.UpgradeService.StageLocalUpgradeArtifact(requestID, sourcePath)
}

func upgradeLocalConfirmLines(binaryPath string) []string {
	return appupgradecmd.UpgradeLocalConfirmLines(binaryPath)
}
