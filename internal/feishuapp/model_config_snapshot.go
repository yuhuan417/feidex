package feishuapp

import (
	configadapter "feidex/internal/adapter/config"
	"feidex/internal/adapter/feishu/modelsettings"

	applicationmodelconfig "feidex/internal/application/modelconfig"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"

	domainmodelconfig "feidex/internal/domain/modelconfig"
)

// Resolve from one config, binding and profile revision. Callers must not hold
// ConfigMu; writers of profiles/bindings use the same lock.
func modelConfigSnapshot(a *App, sess *conversation.Session, backend string) domainmodelconfig.Snapshot {
	if a == nil {
		return domainmodelconfig.Snapshot{}
	}
	return newModelSnapshotService(a).TurnSnapshot(backend, sess)
}

func newModelSnapshotService(a *App) applicationmodelconfig.SnapshotService {
	return applicationmodelconfig.SnapshotService{Repository: configadapter.ModelSourceRepository{Config: a.cfg, Mutex: a.ConfigMu(), Scopes: a.State()}}
}

func modelConfigStatus(a *App, sessionKey string) string {
	backend := configuredBackend(a)
	var sess *conversation.Session
	if store := a.State(); store != nil {
		sess = store.Session(normalizeSessionKey(a, sessionKey))
	}
	view := newModelSnapshotService(a).SessionView(backend, sess)
	return modelsettings.RenderStatus(view)
}

func modelConfigReadCopy(a *App) *config.Config {
	a.ConfigMu().RLock()
	defer a.ConfigMu().RUnlock()
	return config.Clone(a.cfg)
}
