package app

import (
	appcore "feidex/internal/app/appcore"
	appservicetiercmd "feidex/internal/app/servicetiercmd"
	"feidex/internal/domain/conversation"
	"feidex/internal/feishu"
)

type serviceTierAppAdapter struct{ *App }

func newServiceTierService(app *App) appservicetiercmd.Service {
	return appservicetiercmd.NewService(serviceTierAppAdapter{App: app})
}

func (a serviceTierAppAdapter) Feishu() appcore.FeishuClient {
	return a.feishu
}

func (a serviceTierAppAdapter) ServiceTierAppState() appservicetiercmd.AppStateProvider {
	return a.State()
}

func (a serviceTierAppAdapter) MenuCardBody(action, body string) string {
	return menuCardBody(action, body)
}

func renderServiceTierMenuCard(a *App, sessionKey string) map[string]any {
	return newServiceTierService(a).RenderMenuCard(sessionKey)
}

func setThreadServiceTier(a *App, sessionKey, threadID, serviceTier string) (*conversation.Session, error) {
	return newServiceTierService(a).SetThreadServiceTier(sessionKey, threadID, serviceTier)
}

func commandFast(a *App, msg *feishu.InboundMessage, args []string) error {
	return newServiceTierService(a).CommandFast(msg, args)
}
