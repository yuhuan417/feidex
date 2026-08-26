package appcore

import (
	"fmt"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

type agentBindingsForChatProvider interface {
	AgentBindingsForChat(chatType, chatID string) []*state.AgentBinding
}

// MakeWorkspaceSelectionKey returns the frontend-scoped session key used to
// store the current workspace selection for a chat/user scope.
func MakeWorkspaceSelectionKey(a AppConfig, chatType, chatID, userID string) string {
	chatType = strings.TrimSpace(chatType)
	chatID = strings.TrimSpace(chatID)
	userID = strings.TrimSpace(userID)
	if chatType == "" || chatID == "" || userID == "" {
		return ""
	}
	frontendID := ""
	if a != nil {
		frontendID = strings.TrimSpace(a.FrontendID())
	}
	if frontendID != "" {
		return fmt.Sprintf("feishu:frontend:%s:%s:%s:workspace:%s", frontendID, chatType, chatID, userID)
	}
	return fmt.Sprintf("feishu:%s:%s:workspace:%s", chatType, chatID, userID)
}

// MakeWorkspaceSelectionKeyForMessage derives the workspace-selection session
// key from an inbound message.
func MakeWorkspaceSelectionKeyForMessage(a AppConfig, msg *feishu.InboundMessage) string {
	if msg == nil {
		return ""
	}
	return MakeWorkspaceSelectionKey(a, msg.ChatType, msg.ChatID, msg.UserID)
}

// MakeWorkspaceSelectionKeyForSession derives the workspace-selection session
// key from a persisted session.
func MakeWorkspaceSelectionKeyForSession(a AppConfig, sess *state.Session) string {
	if sess == nil {
		return ""
	}
	return MakeWorkspaceSelectionKey(a, sess.ChatType, sess.ChatID, sess.OwnerUserID)
}

// ResolveWorkspaceSelectionForMessage returns the currently selected workspace
// for a message scope, falling back to the provided session and finally the
// configured default workspace.
func ResolveWorkspaceSelectionForMessage(a AppConfig, msg *feishu.InboundMessage, fallback *state.Session) string {
	if a == nil {
		if fallback != nil {
			return strings.TrimSpace(fallback.WorkspaceID)
		}
		return "default"
	}
	if store := a.Store(); store != nil {
		if selectionKey := MakeWorkspaceSelectionKeyForMessage(a, msg); selectionKey != "" {
			if sess := store.GetSession(selectionKey); sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
				return strings.TrimSpace(sess.WorkspaceID)
			}
		}
	}
	if fallback != nil && strings.TrimSpace(fallback.WorkspaceID) != "" {
		return strings.TrimSpace(fallback.WorkspaceID)
	}
	return DefaultWorkspaceID(a)
}

// ResolveWorkspaceSelectionForSession returns the currently selected workspace
// for the session's chat/user scope, falling back to the session itself and
// finally the configured default workspace.
func ResolveWorkspaceSelectionForSession(a AppConfig, sess *state.Session) string {
	if a == nil {
		if sess != nil {
			return strings.TrimSpace(sess.WorkspaceID)
		}
		return "default"
	}
	if store := a.Store(); store != nil {
		if selectionKey := MakeWorkspaceSelectionKeyForSession(a, sess); selectionKey != "" {
			if selected := store.GetSession(selectionKey); selected != nil && strings.TrimSpace(selected.WorkspaceID) != "" {
				return strings.TrimSpace(selected.WorkspaceID)
			}
		}
	}
	if sess != nil && strings.TrimSpace(sess.WorkspaceID) != "" {
		return strings.TrimSpace(sess.WorkspaceID)
	}
	return DefaultWorkspaceID(a)
}

// ResolveBindingWorkspaceForSessionKey returns the workspace configured by a
// group chat binding, if the app exposes binding lookup for the current
// frontend. It accepts a session key so group menu cards can resolve binding
// workspace even before a concrete session has been created.
func ResolveBindingWorkspaceForSessionKey(a AppConfig, sessionKey string, sess *state.Session) string {
	provider, ok := a.(agentBindingsForChatProvider)
	if a == nil || !ok {
		return ""
	}
	chatType := ""
	chatID := ""
	bindingID := ""
	if sess != nil {
		chatType = strings.TrimSpace(sess.ChatType)
		chatID = strings.TrimSpace(sess.ChatID)
		bindingID = strings.TrimSpace(sess.BindingID)
	}
	if chatType == "" || chatID == "" {
		_, parsedChatType, parsedChatID, _, _ := ParseSessionKey(sessionKey)
		chatType = FirstNonEmpty(chatType, parsedChatType)
		chatID = FirstNonEmpty(chatID, parsedChatID)
	}
	if chatType == "" && chatID != "" && len(provider.AgentBindingsForChat("group", chatID)) > 0 {
		chatType = "group"
	}
	if !strings.EqualFold(strings.TrimSpace(chatType), "group") || strings.TrimSpace(chatID) == "" {
		return ""
	}
	bindings := provider.AgentBindingsForChat(chatType, chatID)
	if bindingID != "" {
		for _, binding := range bindings {
			if binding == nil || strings.TrimSpace(binding.ID) != bindingID {
				continue
			}
			return strings.TrimSpace(binding.WorkspaceID)
		}
	}
	for _, binding := range bindings {
		if binding == nil {
			continue
		}
		if workspaceID := strings.TrimSpace(binding.WorkspaceID); workspaceID != "" {
			return workspaceID
		}
	}
	return ""
}

// SetWorkspaceSelection persists the current workspace selection for a
// chat/user scope.
func SetWorkspaceSelection(a AppConfig, chatType, chatID, userID, workspaceID string) error {
	if a == nil || a.Store() == nil {
		return nil
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil
	}
	key := MakeWorkspaceSelectionKey(a, chatType, chatID, userID)
	if key == "" {
		return nil
	}
	sess := a.Store().GetSession(key)
	if sess == nil {
		sess = &state.Session{
			Key:         key,
			ChatID:      strings.TrimSpace(chatID),
			ChatType:    strings.TrimSpace(chatType),
			OwnerUserID: strings.TrimSpace(userID),
			Status:      state.SessionStatusIdle.String(),
		}
	}
	sess.ChatID = strings.TrimSpace(chatID)
	sess.ChatType = strings.TrimSpace(chatType)
	sess.OwnerUserID = strings.TrimSpace(userID)
	sess.WorkspaceID = workspaceID
	sess.Status = firstNonEmptySessionStatus(sess.Status)
	trackWorkspaceSelectionRecent(sess, workspaceID)
	return a.Store().UpsertSession(sess)
}

// SetWorkspaceSelectionForMessage persists the current workspace selection for
// the message scope.
func SetWorkspaceSelectionForMessage(a AppConfig, msg *feishu.InboundMessage, workspaceID string) error {
	if msg == nil {
		return nil
	}
	return SetWorkspaceSelection(a, msg.ChatType, msg.ChatID, msg.UserID, workspaceID)
}

// SetWorkspaceSelectionForSession persists the current workspace selection for
// the session scope.
func SetWorkspaceSelectionForSession(a AppConfig, sess *state.Session, workspaceID string) error {
	if sess == nil {
		return nil
	}
	return SetWorkspaceSelection(a, sess.ChatType, sess.ChatID, sess.OwnerUserID, workspaceID)
}

func trackWorkspaceSelectionRecent(sess *state.Session, workspaceID string) {
	if sess == nil {
		return
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return
	}
	filtered := sess.RecentWorkspaceIDs[:0]
	for _, id := range sess.RecentWorkspaceIDs {
		if strings.TrimSpace(id) != workspaceID {
			filtered = append(filtered, id)
		}
	}
	sess.RecentWorkspaceIDs = append([]string{workspaceID}, filtered...)
}

func firstNonEmptySessionStatus(current string) string {
	if strings.TrimSpace(current) != "" {
		return strings.TrimSpace(current)
	}
	return state.SessionStatusIdle.String()
}
