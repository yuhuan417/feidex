package app

import (
	appupgradecmd "feidex/internal/app/upgradecmd"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// upgradeDisplayLocation is a test seam: tests swap it to control the timezone
// used by upgrade card rendering.
var upgradeDisplayLocation = appupgradecmd.DisplayLocation

type appUpgradeService struct {
	inner appupgradecmd.UpgradeService
}

func newAppUpgradeService(app *App) appUpgradeService {
	return appUpgradeService{inner: newUpgradeServiceInner(app)}
}

func (s appUpgradeService) renderUpgradeFailedCard(sessionKey, errText string) map[string]any {
	return s.inner.RenderUpgradeFailedCard(sessionKey, errText)
}

func (s appUpgradeService) renderUpgradeCardForVersion(sessionKey, ownerUserID, requestedVersion string) (map[string]any, error) {
	return s.inner.RenderUpgradeCardForVersion(sessionKey, ownerUserID, requestedVersion)
}

func (s appUpgradeService) renderUpgradeCardForTarget(sessionKey, ownerUserID, requestedVersion string, useDevRelease bool) (map[string]any, error) {
	return s.inner.RenderUpgradeCardForTarget(sessionKey, ownerUserID, requestedVersion, useDevRelease)
}

func (s appUpgradeService) commandUpgrade(msg *feishu.InboundMessage, args []string) error {
	return s.inner.CommandUpgrade(msg, args)
}

func (s appUpgradeService) completeUpgradeAction(action *feishu.CardAction, actionName string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteUpgradeAction(action, actionName)
}

func (s appUpgradeService) completeUpgradeLocalPick(action *feishu.CardAction) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteUpgradeLocalPick(action)
}
