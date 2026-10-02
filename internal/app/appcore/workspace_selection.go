package appcore

import (
	"feidex/internal/application/workspace"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/feishu"
)

type WorkspaceSelectionSource interface {
	WorkspaceSelection() workspace.SelectionService
}

func selectionService(a WorkspaceSelectionSource) workspace.SelectionService {
	if a == nil {
		return workspace.SelectionService{}
	}
	return a.WorkspaceSelection()
}
func MakeWorkspaceSelectionKey(a FrontendIdentity, chatType, chatID, userID string) string {
	var frontend identity.FrontendID
	if a != nil {
		frontend = identity.FrontendID(a.FrontendID())
	}
	return workspace.SelectionKey(frontend, chatType, chatID, userID)
}
func MakeWorkspaceSelectionKeyForMessage(a FrontendIdentity, msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	return MakeWorkspaceSelectionKey(a, msg.ChatType, msg.ChatID, msg.UserID)
}
func MakeWorkspaceSelectionKeyForSession(a FrontendIdentity, sess *conversation.Session) string {
	if sess == nil {
		return ""
	}
	return MakeWorkspaceSelectionKey(a, sess.ChatType, sess.ChatID, sess.OwnerUserID)
}
func ResolveWorkspaceSelectionForMessage(a WorkspaceSelectionSource, msg *feishu.InboundMessage, fallback *conversation.Session) string {
	service := selectionService(a)
	if msg == nil {
		return service.Resolve("", "", "", fallback)
	}
	return service.Resolve(msg.ChatType, msg.ChatID, msg.UserID, fallback)
}
func ResolveWorkspaceSelectionForSession(a WorkspaceSelectionSource, sess *conversation.Session) string {
	return selectionService(a).ResolveSession(sess)
}
func ResolveBindingWorkspaceForSessionKey(a WorkspaceSelectionSource, sessionKey string, sess *conversation.Session) string {
	return selectionService(a).BindingWorkspace(sessionKey, sess)
}
func SetWorkspaceSelection(a WorkspaceSelectionSource, chatType, chatID, userID, workspaceID string) error {
	return selectionService(a).Select(chatType, chatID, userID, workspaceID)
}
func SetWorkspaceSelectionForMessage(a WorkspaceSelectionSource, msg *feishu.InboundMessage, workspaceID string) error {
	if msg == nil {
		return nil
	}
	return SetWorkspaceSelection(a, msg.ChatType, msg.ChatID, msg.UserID, workspaceID)
}
func SetWorkspaceSelectionForSession(a WorkspaceSelectionSource, sess *conversation.Session, workspaceID string) error {
	if sess == nil {
		return nil
	}
	return SetWorkspaceSelection(a, sess.ChatType, sess.ChatID, sess.OwnerUserID, workspaceID)
}
