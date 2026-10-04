package feishuapp

import (
	"context"
	codexadapter "feidex/internal/adapter/backend/codex"
	history "feidex/internal/adapter/feishu/history"
	"feidex/internal/compositionkit"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
)

type historyOutbound struct{ app *App }

func (o historyOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}

func BuildHistory(app *App) history.Service {
	return compositionkit.NewHistory(compositionkit.HistoryDependencies{
		Frontend: identity.FrontendID(app.FrontendID()),
		Context:  app.Context, Outbound: historyOutbound{app: app},
		Repository: app.State(), Backend: func() string { return app.configView().configuredBackend() },
		CodexClient:   func() codexadapter.RPCClient { return app.runtimeView().currentCodexClient() },
		SessionKey:    func(msg *feishu.InboundMessage) string { return app.configView().makeSessionKey(msg) },
		ReplyInThread: func(chatType string) bool { return app.configView().replyInThreadEnabled() },
	})
}
