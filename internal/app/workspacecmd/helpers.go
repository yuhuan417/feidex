package workspacecmd

import (
	"feidex/internal/domain/conversation"
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

func setSelectedWorkspaceForMessage(app workspaceSelectionSource, msg *feishu.InboundMessage, workspaceID string) error {
	if msg == nil {
		return nil
	}
	return app.WorkspaceSelection().Select(msg.ChatType, msg.ChatID, msg.UserID, workspaceID)
}

func setSelectedWorkspaceForSession(app workspaceSelectionSource, sess *conversation.Session, workspaceID string) error {
	if sess == nil {
		return nil
	}
	return app.WorkspaceSelection().Select(sess.ChatType, sess.ChatID, sess.OwnerUserID, workspaceID)
}

type workspaceSwitchSessionService interface {
	SaveSession(sess *conversation.Session) error
	SwitchSessionWorkspace(sess *conversation.Session, workspaceID string)
	ClearSessionLiveThread(sessionKey string)
}

const (
	workspaceSwitchActiveWorkBlockedText  = "当前任务仍在运行，请先等待结束或中断后再切换工作区"
	workspaceSwitchPendingWorkBlockedText = "当前还有待处理消息，请先处理完成后再切换工作区"
)

func workspaceSwitchBlockedReason(sess *conversation.Session, hasInFlight bool) string {
	if sess == nil {
		return ""
	}
	if hasInFlight {
		return workspaceSwitchActiveWorkBlockedText
	}
	if len(sess.Queue) > 0 || len(sess.StagedImages) > 0 {
		return workspaceSwitchPendingWorkBlockedText
	}
	return ""
}

// WorkspaceSwitchBlockedReason reports whether a workspace binding can be
// changed for the given session.
func WorkspaceSwitchBlockedReason(sess *conversation.Session, hasInFlight bool) string {
	return workspaceSwitchBlockedReason(sess, hasInFlight)
}

func applyWorkspaceSwitch(s workspaceSwitchSessionService, sessionKey string, sess *conversation.Session, workspaceID string) error {
	if s == nil || sess == nil {
		return nil
	}
	s.ClearSessionLiveThread(strings.TrimSpace(sessionKey))
	s.SwitchSessionWorkspace(sess, workspaceID)
	return s.SaveSession(sess)
}
