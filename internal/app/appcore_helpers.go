package app

import (
	"feidex/internal/app/appcore"
	"feidex/internal/feishu"
)

// Function aliases — appcore shared helpers
var (
	feishuConfigUnlocked     = appcore.FeishuConfigUnlocked
	feishuConfig             = appcore.FeishuConfig
	replyInThreadEnabled     = appcore.ReplyInThreadEnabled
	configuredBackend        = appcore.ConfiguredBackend
	currentRuntimeBackend    = appcore.CurrentRuntimeBackend
	hasConfiguredBackend     = appcore.HasConfiguredBackend
	defaultWorkspaceID       = appcore.DefaultWorkspaceID
	normalizeRuntimeBackend  = appcore.NormalizeRuntimeBackend
	parseSessionKey          = appcore.ParseSessionKey
	normalizeSessionKey      = appcore.NormalizeSessionKey
	sessionBelongsToFrontend = appcore.SessionBelongsToFrontend
)

// makeSessionKey wraps appcore.MakeSessionKey with the correct signature.
func makeSessionKey(a *App, msg *feishu.InboundMessage) string {
	return appcore.MakeSessionKey(a, msg)
}
