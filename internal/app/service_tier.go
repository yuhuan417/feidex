package app

import (
	"context"
	tier "feidex/internal/adapter/feishu/servicetier"
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

func newServiceTierService(a *App) tier.Service {
	return tier.Service{Service: threadsettings.Service{Repository: a.State()}, Context: a.Context, Outbound: serviceTierOutbound{app: a}, SessionKey: func(msg *feishu.InboundMessage) string { return makeSessionKey(a, msg) }}
}
func renderServiceTierMenuCard(a *App, key string) map[string]any {
	return tier.RenderMenuCard(key, a.State().Session(key))
}
func setThreadServiceTier(a *App, key, threadID, value string) (*conversation.Session, error) {
	return threadsettings.Service{Repository: a.State()}.SetThreadServiceTier(key, threadID, value)
}
func commandFast(a *App, msg *feishu.InboundMessage, args []string) error {
	return newServiceTierService(a).CommandFast(msg, args)
}
