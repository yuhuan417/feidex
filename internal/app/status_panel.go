package app

import (
	"context"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func (a *App) renderStatusCard(sessionKey string) map[string]any {
	var sess *state.Session
	if strings.TrimSpace(sessionKey) != "" {
		sess = a.appState().session(sessionKey)
	}
	buttons := []feishu.Button{
		{Text: commandLabel("刷新", "/status"), Type: "default", Value: map[string]any{"action": "menu.status", "session_key": sessionKey}},
		{Text: "返回上一级", Type: "default", Value: map[string]any{"action": "menu.group.system", "session_key": sessionKey}},
	}
	return a.feishu.SimpleStatusCard("Status", "blue", menuCardBodyForBackend(a.configuredBackend(), "menu.status", newBackendConfigurationService(a).statusCardBody(sess)), buttons)
}

func (a *App) commandStatus(msg *feishu.InboundMessage) error {
	card := a.renderStatusCard(a.makeSessionKey(msg))
	_, err := a.feishu.ReplyCard(context.Background(), msg.MessageID, card, a.replyInThreadEnabled(msg.ChatType))
	return err
}
