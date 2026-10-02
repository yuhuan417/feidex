package app

import (
	codexadapter "feidex/internal/adapter/backend/codex"
	history "feidex/internal/adapter/feishu/history"
	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/feishu"
)

func newHistoryService(app *App) history.Service {
	return history.NewService(history.Dependencies{
		Context: app.Context, Feishu: app.feishu, State: app.State(),
		Reader: func() (history.ThreadHistoryReader, error) {
			return codexadapter.Gateway{Client: currentCodexClient(app)}, nil
		},
		SessionKey:    func(msg *feishu.InboundMessage) string { return makeSessionKey(app, msg) },
		ReplyInThread: func(chatType string) bool { return replyInThreadEnabled(app, chatType) },
		MenuBody:      menuCardBody,
		ThreadLabel:   appthreadmenu.SessionCurrentThreadLabel,
		HistoryIndex: func(key string, ordinal int) (int, error) {
			if configuredBackend(app) == backendClaude {
				return historyTurnIndexForOrdinal(app, key, ordinal)
			}
			return newHistoryService(app).CodexHistoryIndexForOrdinal(key, ordinal)
		},
		RenderHistory: func(key string, page int) (map[string]any, error) {
			if configuredBackend(app) == backendClaude {
				return renderClaudeHistoryCard(app, key, page)
			}
			return newHistoryService(app).RenderCodexHistoryCard(key, page)
		},
		RenderDetail: func(key string, index int) (map[string]any, error) {
			if configuredBackend(app) == backendClaude {
				return renderClaudeHistoryDetailCard(app, key, index)
			}
			return newHistoryService(app).RenderCodexHistoryDetailCard(key, index)
		},
	})
}
