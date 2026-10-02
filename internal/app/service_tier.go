package app

import (
	tier "feidex/internal/adapter/feishu/servicetier"
	"feidex/internal/application/threadsettings"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
)

func newServiceTierService(a *App) tier.Service {
	return tier.Service{Service: threadsettings.Service{Repository: a.State()}, Context: a.Context, Client: a.feishu, SessionKey: func(msg *feishu.InboundMessage) string { return makeSessionKey(a, msg) }}
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
