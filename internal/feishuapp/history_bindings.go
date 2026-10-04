package feishuapp

import (
	"context"
	codexadapter "feidex/internal/adapter/backend/codex"
	history "feidex/internal/adapter/feishu/history"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	"feidex/internal/application"
	historyapp "feidex/internal/application/history"
	"feidex/internal/compositionkit"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	"feidex/internal/runtime"
)

type historyOutbound struct {
	frontend identity.FrontendID
	runner   runtime.EffectRunner
}

func (o historyOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return o.runner.RunSendCard(ctx, application.SendCard{
		Frontend: o.frontend, ReplyMessageID: messageID,
		View: feishuoutbound.Card(card), InThread: inThread,
	})
}

func BuildHistory(frontend identity.FrontendID, repository historyapp.Repository, backend func() string, codexClient func() codexadapter.RPCClient, ctx func() context.Context, runner runtime.EffectRunner, sessionKey func(*feishu.InboundMessage) string, replyInThread func(string) bool) history.Service {
	return compositionkit.NewHistory(compositionkit.HistoryDependencies{
		Frontend: frontend, Context: ctx, Outbound: historyOutbound{frontend: frontend, runner: runner},
		Repository: repository, Backend: backend, CodexClient: codexClient,
		SessionKey: sessionKey, ReplyInThread: replyInThread,
	})
}
