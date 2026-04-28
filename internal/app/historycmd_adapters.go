package app

import (
	appcore "feidex/internal/app/appcore"
	apphistorycmd "feidex/internal/app/historycmd"
	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

// ---------------------------------------------------------------------------
// Adapter methods on *App — satisfy historycmd.App interface
// ---------------------------------------------------------------------------

// HistoryFeishu returns the Feishu bot client for the history service.
func (a *App) HistoryFeishu() appcore.FeishuClient {
	return a.feishu
}

// HistoryAppState returns the narrowed app state provider for history ops.
func (a *App) HistoryAppState() apphistorycmd.AppStateProvider {
	return a.State()
}

// HistoryConversationBackend returns the narrowed conversation backend
// provider for the history service.
func (a *App) HistoryConversationBackend() apphistorycmd.ConversationBackendProvider {
	return historyConversationBackendAdapter{app: a}
}

// HistoryCodexClient returns the current Codex RPC client.
func (a *App) HistoryCodexClient() (apphistorycmd.CodexClient, error) {
	return requireCodexClient(a)
}

// HistoryMakeSessionKey builds a session key from an inbound message.
func (a *App) HistoryMakeSessionKey(msg *feishu.InboundMessage) string {
	return makeSessionKey(a, msg)
}

// HistoryReplyInThreadEnabled reports whether reply-in-thread is enabled
// for the given chat type.
func (a *App) HistoryReplyInThreadEnabled(chatType string) bool {
	return replyInThreadEnabled(a, chatType)
}

// HistoryMenuCardBody formats a menu card body with breadcrumb navigation.
func (a *App) HistoryMenuCardBody(action, body string) string {
	return menuCardBody(action, body)
}

// HistoryCurrentThreadLabel returns the display label for the active thread.
func (a *App) HistoryCurrentThreadLabel(sess *state.Session) string {
	return appthreadmenu.SessionCurrentThreadLabel(sess)
}

// ---------------------------------------------------------------------------
// Internal adapter types
// ---------------------------------------------------------------------------

// historyConversationBackendAdapter wraps the conversation backend facade to
// satisfy historycmd.ConversationBackendProvider.
type historyConversationBackendAdapter struct {
	app *App
}

func (a historyConversationBackendAdapter) HistoryIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return conversationBackend(a.app).historyIndexForOrdinal(sessionKey, ordinal)
}

func (a historyConversationBackendAdapter) RenderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return conversationBackend(a.app).renderHistoryCard(sessionKey, page)
}

func (a historyConversationBackendAdapter) RenderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return conversationBackend(a.app).renderHistoryDetailCard(sessionKey, index)
}
