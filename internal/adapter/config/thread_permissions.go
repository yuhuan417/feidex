package config

import (
	"feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application/threadsettings"
	fileconfig "feidex/internal/config"
)

type ThreadPermissionRepository struct {
	Source Source
	Scope  *appstate.Store
}

func (r ThreadPermissionRepository) PermissionRevision(key string) threadsettings.PermissionRevision {
	r.Source.ConfigMu().RLock()
	defer r.Source.ConfigMu().RUnlock()
	result := threadsettings.PermissionRevision{Session: r.Scope.Session(key), Profile: r.Scope.BotProfile()}
	if result.Session != nil {
		result.Binding = r.Scope.AgentBinding(result.Session.BindingID)
		if ws := fileconfig.FindWorkspace(r.Source.Config(), result.Session.WorkspaceID); ws != nil {
			copy := *ws
			result.Workspace = &copy
		}
	}
	result.Default = r.Source.Config().Claude.PermissionMode
	result.AllowBypass = r.Source.Config().Claude.DangerouslySkipPermissions
	return result
}
