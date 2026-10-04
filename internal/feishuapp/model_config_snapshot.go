package feishuapp

import (
	"sync"

	configadapter "feidex/internal/adapter/config"
	"feidex/internal/adapter/feishu/modelsettings"
	appstate "feidex/internal/adapter/storage/json/scoped"

	applicationmodelconfig "feidex/internal/application/modelconfig"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"

	domainmodelconfig "feidex/internal/domain/modelconfig"
)

// Resolve from one config, binding and profile revision. Callers must not hold
// ConfigMu; writers of profiles/bindings use the same lock.
func modelConfigSnapshot(snapshots applicationmodelconfig.SnapshotService, sess *conversation.Session, backend string) domainmodelconfig.Snapshot {
	return snapshots.TurnSnapshot(backend, sess)
}

func ModelSnapshotRepository(cfg *config.Config, mu *sync.RWMutex, scopes *appstate.Store) applicationmodelconfig.SourceRepository {
	return configadapter.ModelSourceRepository{Config: cfg, Mutex: mu, Scopes: scopes}
}

func modelConfigStatus(snapshots applicationmodelconfig.SnapshotService, store *appstate.Store, view frontendConfigView, sessionKey string) string {
	var sess *conversation.Session
	if store != nil {
		sess = store.Session(view.normalizeSessionKey(sessionKey))
	}
	return modelsettings.RenderStatus(snapshots.SessionView(view.configuredBackend(), sess))
}

func configReadCopy(cfg *config.Config, mu *sync.RWMutex) *config.Config {
	if mu != nil {
		mu.RLock()
		defer mu.RUnlock()
	}
	return config.Clone(cfg)
}

func CodexServiceName(cfg *config.Config, mu *sync.RWMutex) func() string {
	return func() string { return configReadCopy(cfg, mu).Codex.ServiceName }
}
