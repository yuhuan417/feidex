package feishuapp

import (
	"feidex/internal/adapter/feishu/planmode"

	"feidex/internal/feishu"
)

func renderStatusCard(state planmode.StateProvider, client FeishuClient, backendKind string, statusBody string, sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: commandLabel("刷新", "/status"), Type: "default", Value: map[string]any{"action": "menu.status", "session_key": sessionKey}},
		{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	title := planmode.ContentCardTitleForSessionFromState(state, state != nil, sessionKey, "", "Status")
	return client.SimpleStatusCard(title, "blue", menuCardBodyForBackend(backendKind, "menu.status", statusBody), buttons)
}

func commandStatus(a *App, msg *feishu.InboundMessage) error {
	sessionKey := a.configView().makeSessionKey(msg)
	sess := a.State().Session(sessionKey)
	card := renderStatusCard(a.State(), a.feishu, a.configView().configuredBackend(), a.bindings.BackendConfiguration.StatusCardBody(sess), sessionKey)
	return replyCardEffect(newEffectRunner(a.runtimeOwner), a.FrontendID(), a.configView().replyInThreadEnabled(), msg, card)
}
