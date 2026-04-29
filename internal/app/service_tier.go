package app

import (
	appcore "feidex/internal/app/appcore"
	appservicetiercmd "feidex/internal/app/servicetiercmd"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

const serviceTierFast = appservicetiercmd.ServiceTierFast

var normalizeServiceTier = appservicetiercmd.NormalizeServiceTier

var toggleServiceTier = appservicetiercmd.ToggleServiceTier

var renderServiceTierValue = appservicetiercmd.RenderServiceTierValue

var renderServiceTierReplyValue = appservicetiercmd.RenderServiceTierReplyValue

type serviceTierAppAdapter struct{ *App }

func newServiceTierServiceInner(app *App) appservicetiercmd.Service {
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
	return newServiceTierServiceInner(a).RenderMenuCard(sessionKey)
}

func setThreadServiceTier(a *App, sessionKey, threadID, serviceTier string) (*state.Session, error) {
	return newServiceTierServiceInner(a).SetThreadServiceTier(sessionKey, threadID, serviceTier)
}

func commandFast(a *App, msg *feishu.InboundMessage, args []string) error {
	return newServiceTierServiceInner(a).CommandFast(msg, args)
}
