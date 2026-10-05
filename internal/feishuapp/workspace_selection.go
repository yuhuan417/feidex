package feishuapp

import (
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/workspace"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/textutil"
	"strings"
	"sync"

	"feidex/internal/feishu"
	"feidex/internal/state"
)

func makeWorkspaceSelectionKey(frontendid string, chatType, chatID, userID string) string {
	return workspace.SelectionKey(identity.FrontendID(frontendid), chatType, chatID, userID)
}

func resolveWorkspaceSelectionForMessage(workspaceselection workspace.SelectionService, msg *feishu.InboundMessage, fallback *conversation.Session) string {
	if msg == nil {
		return workspaceselection.Resolve("", "", "", fallback)
	}
	return workspaceselection.Resolve(msg.ChatType, msg.ChatID, msg.UserID, fallback)
}

func setWorkspaceSelectionForMessage(workspaceselection workspace.SelectionService, msg *feishu.InboundMessage, workspaceID string) error {
	if msg == nil {
		return nil
	}
	return workspaceselection.Select(msg.ChatType, msg.ChatID, msg.UserID, workspaceID)
}

func resolveThreadWorkspaceID(sess *conversation.Session, fallback string) string {
	if sess == nil {
		return strings.TrimSpace(fallback)
	}
	return textutil.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(fallback))
}

func resolveSubmissionWorkspaceID(store *appstate.Store, selection workspace.SelectionService, defaultWorkspaceID func() string, msg *feishu.InboundMessage, sess *conversation.Session, bindOnlyCurrentRoot bool) string {
	if binding := agentBindingForSession(store, sess); binding != nil {
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
			resolveWorkspaceSelectionForMessage(selection, msg, sess),
			defaultWorkspaceID(),
		)
	}
	return textutil.FirstNonEmpty(
		resolveWorkspaceSelectionForMessage(selection, msg, sess),
		strings.TrimSpace(func() string {
			if sess == nil {
				return ""
			}
			return sess.WorkspaceID
		}()),
		defaultWorkspaceID(),
	)
}

func agentBindingForSession(store *appstate.Store, sess *conversation.Session) *state.AgentBinding {
	if store == nil || sess == nil {
		return nil
	}
	bindingID := strings.TrimSpace(sess.BindingID)
	if bindingID == "" {
		return nil
	}
	return store.AgentBinding(bindingID)
}

func agentBindingForChat(store *appstate.Store, chatType, chatID string) *state.AgentBinding {
	if store == nil {
		return nil
	}
	bindings := store.AgentBindingsForChat(chatType, chatID)
	for _, binding := range bindings {
		if binding == nil {
			continue
		}
		return binding
	}
	return nil
}

func DefaultWorkspaceID(cfg *config.Config, mu *sync.RWMutex) func() string {
	view := frontendConfigView{cfg: cfg, mu: mu}
	return view.defaultWorkspaceID
}
