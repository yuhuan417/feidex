package feishuapp

import (
	"feidex/internal/domain/conversation"
	"strings"

	"feidex/internal/feishu"
)

func renderStatusCard(a *App, sessionKey string) map[string]any {
	var sess *conversation.Session
	if strings.TrimSpace(sessionKey) != "" {
		sess = a.State().Session(sessionKey)
	}
	buttons := []feishu.Button{
		{Text: commandLabel("刷新", "/status"), Type: "default", Value: map[string]any{"action": "menu.status", "session_key": sessionKey}},
		{Text: feishu.MenuBackButtonText, Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard(planModeTitleForSession(a, sessionKey, "Status"), "blue", menuCardBodyForBackendForSession(a, sessionKey, a.configView().configuredBackend(), "menu.status", a.bindings.BackendConfiguration.StatusCardBody(sess)), buttons)
}

func commandStatus(a *App, msg *feishu.InboundMessage) error {
	card := renderStatusCard(a, a.configView().makeSessionKey(msg))
	return replyCardEffect(a, msg, card)
}
