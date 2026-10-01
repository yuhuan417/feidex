package app

import (
	appdebugviewcmd "feidex/internal/app/debugviewcmd"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func newDebugService(a *App) appdebugviewcmd.DebugService {
	return newDebugServiceInner(a)
}

func newUsageService(a *App) appdebugviewcmd.UsageService {
	return newUsageServiceInner(a)
}

var (
	renderDownloadDisplayPath    = appdebugviewcmd.RenderDownloadDisplayPath
	formatDownloadSize           = appdebugviewcmd.FormatDownloadSize
	runtimeLogLevelText          = appdebugviewcmd.RuntimeLogLevelText
	actionUserID                 = appdebugviewcmd.ActionUserID
	newDownloadPathPickerPayload = appdebugviewcmd.NewDownloadPathPickerPayload
)

func commandDownload(a *App, msg *feishu.InboundMessage, args []string) error {
	return appdebugviewcmd.CommandDownload(newDebugViewAppAdapter(a), msg, args)
}

func completeMenuDownload(a *App, action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return appdebugviewcmd.CompleteMenuDownload(newDebugViewAppAdapter(a), action, sessionKey)
}

func completeDownloadFileConfirm(a *App, action *feishu.CardAction, pending *state.PendingRequest, payload appdebugviewcmd.PathPickerPayload, selectedPath string) (*callback.CardActionTriggerResponse, error) {
	return appdebugviewcmd.CompleteDownloadFileConfirm(newDebugViewAppAdapter(a), action, pending, payload, selectedPath)
}

func finishDownloadFileShare(a *App, requestID, messageID string, payload appdebugviewcmd.PathPickerPayload, selectedPath, workspaceCWD string, req feishu.SharedFileRequest) {
	appdebugviewcmd.FinishDownloadFileShare(newDebugViewAppAdapter(a), requestID, messageID, payload, selectedPath, workspaceCWD, req)
}

func renderDownloadFailedCard(a *App, selectedPath, workspaceCWD, errText string) map[string]any {
	return appdebugviewcmd.RenderDownloadFailedCard(newDebugViewAppAdapter(a), selectedPath, workspaceCWD, errText)
}
