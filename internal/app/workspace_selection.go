package app

import (
	"strings"

	"feidex/internal/app/appcore"
	"feidex/internal/app/apputil"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func makeWorkspaceSelectionKey(a *App, chatType, chatID, userID string) string {
	return appcore.MakeWorkspaceSelectionKey(a, chatType, chatID, userID)
}

func resolveWorkspaceSelectionForMessage(a *App, msg *feishu.InboundMessage, fallback *state.Session) string {
	return appcore.ResolveWorkspaceSelectionForMessage(a, msg, fallback)
}

func setWorkspaceSelectionForMessage(a *App, msg *feishu.InboundMessage, workspaceID string) error {
	return appcore.SetWorkspaceSelectionForMessage(a, msg, workspaceID)
}

func resolveThreadWorkspaceID(sess *state.Session, fallback string) string {
	if sess == nil {
		return strings.TrimSpace(fallback)
	}
	return apputil.FirstNonEmpty(strings.TrimSpace(sess.ActiveThreadWorkspaceID), strings.TrimSpace(sess.WorkspaceID), strings.TrimSpace(fallback))
}

func resolveSubmissionWorkspaceID(a *App, msg *feishu.InboundMessage, sess *state.Session, bindOnlyCurrentRoot bool) string {
	if binding := agentBindingForSession(a, sess); binding != nil {
		if bindOnlyCurrentRoot {
			if workspaceID := strings.TrimSpace(sess.ActiveThreadWorkspaceID); workspaceID != "" {
				return workspaceID
			}
		}
		return strings.TrimSpace(binding.WorkspaceID)
	}
	if bindOnlyCurrentRoot {
		return apputil.FirstNonEmpty(
			resolveThreadWorkspaceID(sess, ""),
			resolveWorkspaceSelectionForMessage(a, msg, sess),
			defaultWorkspaceID(a),
		)
	}
	return apputil.FirstNonEmpty(
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

func agentBindingForSession(a *App, sess *state.Session) *state.AgentBinding {
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
