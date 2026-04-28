package app

import appdebugviewcmd "feidex/internal/app/debugviewcmd"

type debugService = appdebugviewcmd.DebugService

type usageService = appdebugviewcmd.UsageService

func newDebugService(a *App) appdebugviewcmd.DebugService {
	return appdebugviewcmd.NewDebugService(a)
}

func newUsageService(a *App) appdebugviewcmd.UsageService {
	return appdebugviewcmd.NewUsageService(a)
}

var (
	commandDownload                 = appdebugviewcmd.CommandDownload
	completeMenuDownload            = appdebugviewcmd.CompleteMenuDownload
	completeDownloadFileConfirm     = appdebugviewcmd.CompleteDownloadFileConfirm
	finishDownloadFileShare         = appdebugviewcmd.FinishDownloadFileShare
	renderDownloadPreparingCard     = appdebugviewcmd.RenderDownloadPreparingCard
	renderDownloadReadyCard         = appdebugviewcmd.RenderDownloadReadyCard
	renderDownloadFailedCard        = appdebugviewcmd.RenderDownloadFailedCard
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
