package app

import (
	appdebugviewcmd "feidex/internal/app/debugviewcmd"
	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// ---------------------------------------------------------------------------
// Adapter methods on *App — satisfy debugviewcmd.App interface
// ---------------------------------------------------------------------------

// DebugFeishu returns the Feishu bot client for the debug/usage/download services.
func (a *App) DebugFeishu() appdebugviewcmd.FeishuClient {
	return a.feishu
}

// DebugAppState returns the narrowed app state provider for debug/usage/download ops.
func (a *App) DebugAppState() appdebugviewcmd.AppStateProvider {
	return a.State()
}

// DebugRuntimeState returns the narrowed runtime state provider for usage ops.
func (a *App) DebugRuntimeState() appdebugviewcmd.RuntimeStateProvider {
	return debugRuntimeStateAdapter{app: a}
}

// debugRuntimeStateAdapter wraps runtimeStateService to satisfy
// debugviewcmd.RuntimeStateProvider (return type mismatch on TurnBindingTracker).
type debugRuntimeStateAdapter struct {
	app *App
}

func (a debugRuntimeStateAdapter) TurnBindingTracker() appdebugviewcmd.TurnBindingTracker {
	return newRuntimeStateService(a.app).turnBindingTracker()
}

func (a debugRuntimeStateAdapter) CurrentThreadUsage(threadID string) (codexrpc.ThreadTokenUsage, bool) {
	return newRuntimeStateService(a.app).currentThreadUsage(threadID)
}

// DebugConversationBackend returns the narrowed conversation backend provider
// for usage rendering.
func (a *App) DebugConversationBackend() appdebugviewcmd.ConversationBackendProvider {
	return debugConversationBackendAdapter{app: a}
}

// DebugWorkspaceConfig returns the narrowed workspace config provider.
func (a *App) DebugWorkspaceConfig() appdebugviewcmd.WorkspaceConfigProvider {
	return debugWorkspaceConfigAdapter{app: a}
}

// DebugWorkspaceRender returns the narrowed workspace render provider.
func (a *App) DebugWorkspaceRender() appdebugviewcmd.WorkspaceRenderProvider {
	return debugWorkspaceRenderAdapter{app: a}
}

// DebugMakeSessionKey builds a session key from an inbound message.
func (a *App) DebugMakeSessionKey(msg *feishu.InboundMessage) string {
	return makeSessionKey(a, msg)
}

// DebugReplyInThreadEnabled reports whether reply-in-thread is enabled.
func (a *App) DebugReplyInThreadEnabled(chatType string) bool {
	return replyInThreadEnabled(a, chatType)
}

// DebugCompleteMenuCommand dispatches a menu command from a card action.
func (a *App) DebugCompleteMenuCommand(action *feishu.CardAction, sessionKey, rawCommand, parentAction string) (*callback.CardActionTriggerResponse, error) {
	return completeMenuCommand(a, action, sessionKey, rawCommand, parentAction)
}

// DebugMenuCardBody formats a menu card body with breadcrumb navigation.
func (a *App) DebugMenuCardBody(action, body string) string {
	return menuCardBody(action, body)
}

// DebugMenuBreadcrumbLabels returns breadcrumb labels for a menu action.
func (a *App) DebugMenuBreadcrumbLabels(action string) []string {
	return menuBreadcrumbLabels(action)
}

// DebugCommandLabel formats a command label with its slash command.
func (a *App) DebugCommandLabel(label, slash string) string {
	return commandLabel(label, slash)
}

// DebugCurrentThreadLabel returns the display label for the active thread.
func (a *App) DebugCurrentThreadLabel(sess *state.Session) string {
	return appthreadmenu.SessionCurrentThreadLabel(sess)
}

// DebugPrimaryConversationMissingLabel returns the missing conversation label
// for the given backend.
func (a *App) DebugPrimaryConversationMissingLabel(backend string) string {
	return primaryConversationMissingLabel(backend)
}

// DebugDefaultWorkspaceID returns the default workspace ID.
func (a *App) DebugDefaultWorkspaceID() string {
	return defaultWorkspaceID(a)
}

// DebugConfigPath returns the config file path.
func (a *App) DebugConfigPath() string {
	return a.cfgPath
}

// debugConversationBackendAdapter wraps the conversation backend facade to
// satisfy debugviewcmd.ConversationBackendProvider.
type debugConversationBackendAdapter struct {
	app *App
}

func (a debugConversationBackendAdapter) RenderUsageBody(sess *state.Session) string {
	return conversationBackend(a.app).RenderUsageBody(sess)
}

// debugWorkspaceConfigAdapter wraps workspace config operations to satisfy
// debugviewcmd.WorkspaceConfigProvider.
type debugWorkspaceConfigAdapter struct {
	app *App
}

func (a debugWorkspaceConfigAdapter) CurrentWorkspaceForMessage(msg *feishu.InboundMessage) (string, *state.Session, *config.Workspace) {
	return newWorkspaceConfigService(a.app).currentWorkspaceForMessage(msg)
}

// debugWorkspaceRenderAdapter wraps workspace render operations to satisfy
// debugviewcmd.WorkspaceRenderProvider.
type debugWorkspaceRenderAdapter struct {
	app *App
}

func (a debugWorkspaceRenderAdapter) RenderPathPickerCard(requestID string, payload appdebugviewcmd.PathPickerPayload) (map[string]any, error) {
	return newWorkspaceRenderService(a.app).renderPathPickerCard(requestID, payload)
}

// ---------------------------------------------------------------------------
// Type aliases and function aliases for backward compatibility
// ---------------------------------------------------------------------------

// debugService is the type alias for backward compatibility.
type debugService = appdebugviewcmd.DebugService

// usageService is the type alias for backward compatibility.
type usageService = appdebugviewcmd.UsageService

// newDebugService creates a debugService backed by *App.
func newDebugService(a *App) appdebugviewcmd.DebugService {
	return appdebugviewcmd.NewDebugService(a)
}

// newUsageService creates a usageService backed by *App.
func newUsageService(a *App) appdebugviewcmd.UsageService {
	return appdebugviewcmd.NewUsageService(a)
}

// Function aliases for exported download functions.
var (
	commandDownload             = appdebugviewcmd.CommandDownload
	completeMenuDownload        = appdebugviewcmd.CompleteMenuDownload
	completeDownloadFileConfirm = appdebugviewcmd.CompleteDownloadFileConfirm
	finishDownloadFileShare     = appdebugviewcmd.FinishDownloadFileShare
)

// Function aliases for rendering helpers.
var (
	renderDownloadPreparingCard = appdebugviewcmd.RenderDownloadPreparingCard
	renderDownloadReadyCard     = appdebugviewcmd.RenderDownloadReadyCard
	renderDownloadFailedCard    = appdebugviewcmd.RenderDownloadFailedCard
	renderDownloadDisplayPath   = appdebugviewcmd.RenderDownloadDisplayPath
	formatDownloadSize          = appdebugviewcmd.FormatDownloadSize
)

// Function/variable aliases for debug helpers.
var (
	debugLogRecentLimit         = appdebugviewcmd.DebugLogRecentLimit
	debugLogCardMaxChars        = appdebugviewcmd.DebugLogCardMaxChars
	debugLogPreviewAction       = appdebugviewcmd.DebugLogPreviewAction
	debugAccessUnauthorizedText = appdebugviewcmd.DebugAccessUnauthorizedText
	runtimeLogLevelText         = appdebugviewcmd.RuntimeLogLevelText
	desiredDebugEnabled         = appdebugviewcmd.DesiredDebugEnabled
	debugUserAllowed            = appdebugviewcmd.DebugUserAllowed
	actionUserID                = appdebugviewcmd.ActionUserID
	renderRuntimeLogLevelValue  = appdebugviewcmd.RenderRuntimeLogLevelValue
	compactDebugLogText         = appdebugviewcmd.CompactDebugLogText
	debugLogPlainTextBlock      = appdebugviewcmd.DebugLogPlainTextBlock
)

// Function aliases for usage helpers.
var (
	formatUsageInt                  = appdebugviewcmd.FormatUsageInt
	formatUsageRatio                = appdebugviewcmd.FormatUsageRatio
	formatUsageCost                 = appdebugviewcmd.FormatUsageCost
	formatTurnUsageLine             = appdebugviewcmd.FormatTurnUsageLine
	formatTurnElapsedLine           = appdebugviewcmd.FormatTurnElapsedLine
	formatContextLeftLine           = appdebugviewcmd.FormatContextLeftLine
	formatContextUsedLine           = appdebugviewcmd.FormatContextUsedLine
	renderThreadUsageCardBody       = appdebugviewcmd.RenderThreadUsageCardBody
	renderClaudeThreadUsageCardBody = appdebugviewcmd.RenderClaudeThreadUsageCardBody
)

// Path picker function alias.
var newDownloadPathPickerPayload = appdebugviewcmd.NewDownloadPathPickerPayload

// downloadFilePendingKind is the pending request kind for download.
const downloadFilePendingKind = "download_file"

// rawCard wraps a card map for CardActionTriggerResponse.
func rawCard(card map[string]any) *callback.Card {
	return appdebugviewcmd.RawCard(card)
}

// mustJSON marshals v to JSON string, ignoring errors.
func mustJSON(v any) string {
	return appdebugviewcmd.MustJSON(v)
}
