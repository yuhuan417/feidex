package config

import (
	"path/filepath"
	"strings"
	"sync"

	workspaceapp "feidex/internal/application/workspace"
	fileconfig "feidex/internal/config"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/identity"
	"feidex/internal/domain/routing"
	domain "feidex/internal/domain/workspace"
)

type WorkspaceViewScopes interface {
	Session(string) *conversation.Session
	BotProfile() *routing.BotProfile
	AgentBindingsForChat(string, string) []*routing.AgentBinding
}

type WorkspaceViewRepository struct {
	Config     *fileconfig.Config
	ConfigPath string
	Backend    func() string
	Mutex      *sync.RWMutex
	Scopes     WorkspaceViewScopes
}

func (r WorkspaceViewRepository) WorkspaceViewSource(frontend identity.FrontendID, key string) workspaceapp.ViewSource {
	if r.Mutex != nil {
		r.Mutex.RLock()
		defer r.Mutex.RUnlock()
	}
	result := workspaceapp.ViewSource{ConfigDirectory: filepath.Dir(r.ConfigPath)}
	parsedFrontend, chatType, chatID, _, _ := identity.ParseSessionKey(key)
	if parsedFrontend != "" && parsedFrontend != string(frontend) && !(frontend == "" && parsedFrontend == fileconfig.DefaultFrontendID) {
		return workspaceapp.ViewSource{}
	}
	if r.Backend != nil {
		result.Backend = r.Backend()
	}
	if r.Config != nil {
		result.ClaudePermissionMode = r.Config.Claude.PermissionMode
		result.ClaudeBypassEnabled = r.Config.Claude.DangerouslySkipPermissions
	}
	if r.Config != nil {
		result.Workspaces = append([]domain.Workspace(nil), r.Config.Workspaces...)
	}
	if r.Scopes == nil {
		return result
	}
	result.Session = r.Scopes.Session(key)
	if sess := result.Session; sess != nil {
		if chatType == "" {
			chatType = strings.TrimSpace(sess.ChatType)
		}
		if chatID == "" {
			chatID = strings.TrimSpace(sess.ChatID)
		}
		selectionKey := workspaceapp.SelectionKey(frontend, sess.ChatType, sess.ChatID, sess.OwnerUserID)
		if selectionKey != "" {
			result.Selection = r.Scopes.Session(selectionKey)
		}
	}
	bindings := r.Scopes.AgentBindingsForChat("group", chatID)
	if chatType == "" && chatID != "" && len(bindings) > 0 {
		chatType = "group"
	}
	result.Group = chatType == "group" && chatID != ""
	if result.Group && len(bindings) > 0 {
		result.Binding = bindings[0]
	}
	result.Profile = r.Scopes.BotProfile()
	return result
}
