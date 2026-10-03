package workspacecmd

import (
	"feidex/internal/domain/conversation"
	"fmt"
	"path/filepath"
	"strings"

	workspaceapp "feidex/internal/application/workspace"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func rawCard(card map[string]any) *callback.Card {
	return &callback.Card{Type: "raw", Data: card}
}

func sameWorkspaceCWD(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
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

func selectedWorkspaceIDForSession(app workspaceSelectionSource, sess *conversation.Session) string {
	return app.WorkspaceSelection().ResolveSession(sess)
}

func applyWorkspaceSwitch(lifecycle *workspaceapp.Lifecycle, clear func(string), sess *conversation.Session, workspaceID string) error {
	if lifecycle == nil {
		return fmt.Errorf("workspace lifecycle is unavailable")
	}
	effects, err := lifecycle.Switch(workspaceapp.SwitchRequest{Session: sess, WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	if effects.Session != nil {
		*sess = *effects.Session
	}
	for _, key := range effects.ClearLiveThreads {
		clear(key)
	}
	return nil
}
