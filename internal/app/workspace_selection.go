package app

import (
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/workspace"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/textutil"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func makeWorkspaceSelectionKey(a *App, chatType, chatID, userID string) string {
	return workspace.SelectionKey(identity.FrontendID(a.FrontendID()), chatType, chatID, userID)
}

func resolveWorkspaceSelectionForMessage(a *App, msg *feishu.InboundMessage, fallback *conversation.Session) string {
	if msg == nil {
		return a.WorkspaceSelection().Resolve("", "", "", fallback)
	}
	return a.WorkspaceSelection().Resolve(msg.ChatType, msg.ChatID, msg.UserID, fallback)
}

func setWorkspaceSelectionForMessage(a *App, msg *feishu.InboundMessage, workspaceID string) error {
	if msg == nil {
		return nil
	}
	return a.WorkspaceSelection().Select(msg.ChatType, msg.ChatID, msg.UserID, workspaceID)
}

func resolveThreadWorkspaceID(sess *conversation.Session, fallback string) string {
	if sess == nil {
		return strings.TrimSpace(fallback)
	}
	return textutil.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(fallback))
}

func resolveSubmissionWorkspaceID(a *App, msg *feishu.InboundMessage, sess *conversation.Session, bindOnlyCurrentRoot bool) string {
	if binding := agentBindingForSession(a, sess); binding != nil {
		if bindOnlyCurrentRoot {
			if workspaceID := strings.TrimSpace(sess.ActiveThreadWorkspaceID); workspaceID != "" {
				return workspaceID
			}
		}
		return strings.TrimSpace(binding.WorkspaceID)
	}
	if bindOnlyCurrentRoot {
		return textutil.FirstNonEmpty(
			resolveThreadWorkspaceID(sess, ""),
			resolveWorkspaceSelectionForMessage(a, msg, sess),
			defaultWorkspaceID(a),
		)
	}
	return textutil.FirstNonEmpty(
		resolveWorkspaceSelectionForMessage(a, msg, sess),
		strings.TrimSpace(func() string {
			if sess == nil {
				return ""
			}
			return sess.WorkspaceID
		}()),
		defaultWorkspaceID(a),
	)
}

func agentBindingForSession(a *App, sess *conversation.Session) *state.AgentBinding {
	if a == nil || sess == nil {
		return nil
	}
	bindingID := strings.TrimSpace(sess.BindingID)
	if bindingID == "" {
		return nil
	}
	return a.State().AgentBinding(bindingID)
}

func agentBindingForChat(a *App, chatType, chatID string) *state.AgentBinding {
	if a == nil {
		return nil
	}
	bindings := a.State().AgentBindingsForChat(chatType, chatID)
	for _, binding := range bindings {
		if binding == nil {
			continue
		}
		return binding
	}
	return nil
}

// WorkspaceSelection binds the use case directly to scoped repositories.
func (a *App) WorkspaceSelection() workspace.SelectionService {
	if a == nil {
		return workspace.SelectionService{}
	}
	return workspace.SelectionService{Frontend: identity.FrontendID(a.FrontendID()), DefaultWorkspace: defaultWorkspaceID(a), Repository: appstate.WorkspaceSelections{Store: a.State()}}
}
