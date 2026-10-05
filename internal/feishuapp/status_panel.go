package feishuapp

import (
	"feidex/internal/adapter/feishu/backend"
	"feidex/internal/adapter/feishu/planmode"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/feishu"
	frontendruntime "feidex/internal/runtime"
)

func renderStatusCard(state planmode.StateProvider, client FeishuClient, backendKind string, statusBody string, sessionKey string) map[string]any {
	buttons := []feishu.Button{
		{Text: commandLabel("刷新", "/status"), Type: "default", Value: map[string]any{"action": "menu.status", "session_key": sessionKey}},
		{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	title := planmode.ContentCardTitleForSessionFromState(state, state != nil, sessionKey, "", "Status")
	return client.SimpleStatusCard(title, "blue", menuCardBodyForBackend(backendKind, "menu.status", statusBody), buttons)
}

func handleStatusCommand(state *appstate.Store, backendKind func() string, configuration backend.ConfigurationService, makeSessionKey func(*feishu.InboundMessage) string, renderer FeishuClient, effects frontendruntime.EffectRunner, frontendID string, replyInThread bool, msg *feishu.InboundMessage) error {
	sessionKey := makeSessionKey(msg)
	sess := state.Session(sessionKey)
	card := renderStatusCard(state, renderer, backendKind(), configuration.StatusCardBody(sess), sessionKey)
	return replyCardEffect(effects, frontendID, replyInThread, msg, card)
}
