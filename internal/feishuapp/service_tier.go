package feishuapp

import (
	"context"
	feishuoutbound "feidex/internal/adapter/feishu/outbound"
	tier "feidex/internal/adapter/feishu/servicetier"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application"
	"feidex/internal/application/threadsettings"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
	"feidex/internal/runtime"
)

type serviceTierOutbound struct {
	frontend identity.FrontendID
	runner   runtime.EffectRunner
}

func (o serviceTierOutbound) ReplyCard(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
	return o.runner.RunSendCard(ctx, application.SendCard{
		Frontend: o.frontend, ReplyMessageID: messageID,
		View: feishuoutbound.Card(card), InThread: inThread,
	})
}
func (o serviceTierOutbound) ReplyText(ctx context.Context, messageID, text string, inThread bool) error {
	_, err := o.runner.RunSendMessage(ctx, application.SendMessage{
		Frontend: o.frontend, ReplyMessageID: messageID, Text: text, InThread: inThread,
	})
	return err
}

func BuildServiceTier(service threadsettings.Service, ctx func() context.Context, frontend identity.FrontendID, runner runtime.EffectRunner, sessionKey func(*feishu.InboundMessage) string) tier.Service {
	return tier.Service{Service: service, Context: ctx, Outbound: serviceTierOutbound{frontend: frontend, runner: runner}, SessionKey: sessionKey}
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
