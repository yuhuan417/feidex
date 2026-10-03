package app

import (
	"context"
	codexadapter "feidex/internal/adapter/backend/codex"
	history "feidex/internal/adapter/feishu/history"
	appthreadmenu "feidex/internal/app/threadmenu"
	"feidex/internal/feishu"
)

type historyOutbound struct{ app *App }

func (o historyOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}

type historyCardRenderer struct{ app *App }

func (r historyCardRenderer) SimpleStatusCard(title, color, body string, buttons []feishu.Button) map[string]any {
	if r.app == nil || r.app.feishu == nil {
		return nil
	}
	return r.app.feishu.SimpleStatusCard(title, color, body, buttons)
}

func newHistoryService(app *App) history.Service {
	return history.NewService(history.Dependencies{
		Context: app.Context, Outbound: historyOutbound{app: app}, Renderer: historyCardRenderer{app: app}, State: app.State(),
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
