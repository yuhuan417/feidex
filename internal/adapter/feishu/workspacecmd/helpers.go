package workspacecmd

import (
	"feidex/internal/domain/conversation"

	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

type workspaceSelectionSource interface {
	WorkspaceSelection() workspaceapp.SelectionService
}

func selectedWorkspaceIDForMessage(app workspaceSelectionSource, msg *feishu.InboundMessage, sess *conversation.Session) string {
	if msg == nil {
		return app.WorkspaceSelection().Resolve("", "", "", sess)
	}
	return app.WorkspaceSelection().Resolve(msg.ChatType, msg.ChatID, msg.UserID, sess)
}
