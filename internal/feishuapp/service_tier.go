package feishuapp

import (
	"context"
	tier "feidex/internal/adapter/feishu/servicetier"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/threadsettings"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
)

type serviceTierOutbound struct{ app *App }

func (o serviceTierOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return replyCardWithIDEffect(ctx, o.app, messageID, card, inThread)
}
func (o serviceTierOutbound) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	return replyTextByAnchorEffect(ctx, o.app, messageID, text, inThread)
}

func BuildServiceTier(a *App) tier.Service {
	return tier.Service{Service: a.bindings.ThreadSettings, Context: a.Context, Outbound: serviceTierOutbound{app: a}, SessionKey: func(msg *feishu.InboundMessage) string { return makeSessionKey(a, msg) }}
}
func renderServiceTierMenuCard(state *appstate.Store, key string) map[string]any {
	return tier.RenderMenuCard(key, state.Session(key))
}
func setThreadServiceTier(threadsettingsDep threadsettings.Service, key, threadID, value string) (*conversation.Session, error) {
	return threadsettingsDep.SetThreadServiceTier(key, threadID, value)
}
func commandFast(servicetier tier.Service, msg *feishu.InboundMessage, args []string) error {
	return servicetier.CommandFast(msg, args)
}
