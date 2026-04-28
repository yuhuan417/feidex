package app

import (
	appdebugviewcmd "feidex/internal/app/debugviewcmd"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type debugService = appdebugviewcmd.DebugService

type usageService = appdebugviewcmd.UsageService

func newDebugService(a *App) appdebugviewcmd.DebugService {
	return newDebugServiceInner(a)
}

func newUsageService(a *App) appdebugviewcmd.UsageService {
	return newUsageServiceInner(a)
}

var (
	renderDownloadDisplayPath       = appdebugviewcmd.RenderDownloadDisplayPath
	formatDownloadSize              = appdebugviewcmd.FormatDownloadSize
	debugLogRecentLimit             = appdebugviewcmd.DebugLogRecentLimit
	debugLogCardMaxChars            = appdebugviewcmd.DebugLogCardMaxChars
	debugLogPreviewAction           = appdebugviewcmd.DebugLogPreviewAction
	debugAccessUnauthorizedText     = appdebugviewcmd.DebugAccessUnauthorizedText
	runtimeLogLevelText             = appdebugviewcmd.RuntimeLogLevelText
	desiredDebugEnabled             = appdebugviewcmd.DesiredDebugEnabled
	debugUserAllowed                = appdebugviewcmd.DebugUserAllowed
	actionUserID                    = appdebugviewcmd.ActionUserID
	renderRuntimeLogLevelValue      = appdebugviewcmd.RenderRuntimeLogLevelValue
	compactDebugLogText             = appdebugviewcmd.CompactDebugLogText
	debugLogPlainTextBlock          = appdebugviewcmd.DebugLogPlainTextBlock
	formatUsageInt                  = appdebugviewcmd.FormatUsageInt
	formatUsageRatio                = appdebugviewcmd.FormatUsageRatio
	formatUsageCost                 = appdebugviewcmd.FormatUsageCost
	formatTurnUsageLine             = appdebugviewcmd.FormatTurnUsageLine
	formatTurnElapsedLine           = appdebugviewcmd.FormatTurnElapsedLine
	formatContextLeftLine           = appdebugviewcmd.FormatContextLeftLine
	formatContextUsedLine           = appdebugviewcmd.FormatContextUsedLine
	renderThreadUsageCardBody       = appdebugviewcmd.RenderThreadUsageCardBody
	renderClaudeThreadUsageCardBody = appdebugviewcmd.RenderClaudeThreadUsageCardBody
	newDownloadPathPickerPayload    = appdebugviewcmd.NewDownloadPathPickerPayload
)

const downloadFilePendingKind = appdebugviewcmd.DownloadFilePendingKind

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

func renderDownloadPreparingCard(a *App, selectedPath, workspaceCWD string) map[string]any {
	return appdebugviewcmd.RenderDownloadPreparingCard(newDebugViewAppAdapter(a), selectedPath, workspaceCWD)
}

func renderDownloadReadyCard(a *App, selectedPath, workspaceCWD string, result feishu.SharedFileResult) map[string]any {
	return appdebugviewcmd.RenderDownloadReadyCard(newDebugViewAppAdapter(a), selectedPath, workspaceCWD, result)
}

func renderDownloadFailedCard(a *App, selectedPath, workspaceCWD, errText string) map[string]any {
	return appdebugviewcmd.RenderDownloadFailedCard(newDebugViewAppAdapter(a), selectedPath, workspaceCWD, errText)
}
