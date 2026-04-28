package app

import (
	appcore "feidex/internal/app/appcore"
	apphistorycmd "feidex/internal/app/historycmd"
	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

type historyAppAdapter struct{ *App }

func newHistoryAppAdapter(app *App) historyAppAdapter {
	return historyAppAdapter{App: app}
}

func newHistoryServiceInner(app *App) apphistorycmd.Service {
	return apphistorycmd.NewService(newHistoryAppAdapter(app))
}

func (a historyAppAdapter) HistoryFeishu() appcore.FeishuClient {
	return a.feishu
}

func (a historyAppAdapter) HistoryAppState() apphistorycmd.AppStateProvider {
	return a.State()
}

func (a historyAppAdapter) HistoryConversationBackend() apphistorycmd.ConversationBackendProvider {
	return historyConversationBackendAdapter{app: a.App}
}

func (a historyAppAdapter) HistoryCodexClient() (apphistorycmd.CodexClient, error) {
	return requireCodexClient(a.App)
}

func (a historyAppAdapter) HistoryMakeSessionKey(msg *feishu.InboundMessage) string {
	return makeSessionKey(a.App, msg)
}

func (a historyAppAdapter) HistoryReplyInThreadEnabled(chatType string) bool {
	return replyInThreadEnabled(a.App, chatType)
}

func (a historyAppAdapter) HistoryMenuCardBody(action, body string) string {
	return menuCardBody(action, body)
}

func (a historyAppAdapter) HistoryCurrentThreadLabel(sess *state.Session) string {
	return appthreadmenu.SessionCurrentThreadLabel(sess)
}

type historyConversationBackendAdapter struct {
	app *App
}

func (a historyConversationBackendAdapter) HistoryIndexForOrdinal(sessionKey string, ordinal int) (int, error) {
	return conversationBackend(a.app).HistoryIndexForOrdinal(sessionKey, ordinal)
}

func (a historyConversationBackendAdapter) RenderHistoryCard(sessionKey string, page int) (map[string]any, error) {
	return conversationBackend(a.app).RenderHistoryCard(sessionKey, page)
}

func (a historyConversationBackendAdapter) RenderHistoryDetailCard(sessionKey string, index int) (map[string]any, error) {
	return conversationBackend(a.app).RenderHistoryDetailCard(sessionKey, index)
}
