package workspacecmd

import (
	"feidex/internal/domain/conversation"
	"path/filepath"
	"strings"

	"feidex/internal/app/appcore"
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

func selectedWorkspaceIDForMessage(app appcore.WorkspaceSelectionSource, msg *feishu.InboundMessage, sess *conversation.Session) string {
	return appcore.ResolveWorkspaceSelectionForMessage(app, msg, sess)
}

func selectedWorkspaceIDForSession(app appcore.WorkspaceSelectionSource, sess *conversation.Session) string {
	return appcore.ResolveWorkspaceSelectionForSession(app, sess)
}

func setSelectedWorkspaceForMessage(app appcore.WorkspaceSelectionSource, msg *feishu.InboundMessage, workspaceID string) error {
	return appcore.SetWorkspaceSelectionForMessage(app, msg, workspaceID)
}

func setSelectedWorkspaceForSession(app appcore.WorkspaceSelectionSource, sess *conversation.Session, workspaceID string) error {
	return appcore.SetWorkspaceSelectionForSession(app, sess, workspaceID)
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
