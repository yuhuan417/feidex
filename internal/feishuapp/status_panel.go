package feishuapp

import (
	"feidex/internal/adapter/feishu/backend"
	appmenuutil "feidex/internal/adapter/feishu/menuutil"
	"feidex/internal/adapter/feishu/planmode"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
)

func renderStatusCard(state planmode.StateProvider, client FeishuClient, backendKind string, statusBody string, sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: commandLabel("刷新", "/status"), Type: "default", Value: map[string]any{"action": "menu.status", "session_key": sessionKey}},
	}
	title := planmode.ContentCardTitleForSessionFromState(state, state != nil, sessionKey, "", "Status")
	_ = client
	return appmenuutil.PageCard{
		Node: "menu.status", Backend: backendKind, SessionKey: sessionKey, Title: title, Color: "blue",
		Body: statusBody, Buttons: buttons,
	}.Render()
}

func handleStatusCommand(state *appstate.Store, backendKind func() string, configuration backend.ConfigurationService, makeSessionKey func(*feishu.InboundMessage) string, renderer FeishuClient, effects frontendruntime.EffectRunner, frontendID string, replyInThread bool, msg *feishu.InboundMessage) error {
	sessionKey := makeSessionKey(msg)
	sess := state.Session(sessionKey)
	card := renderStatusCard(state, renderer, backendKind(), configuration.StatusCardBody(sess), sessionKey)
	return replyCardEffect(effects, frontendID, replyInThread, msg, card)
}
