package feishuapp

import (
	appworkspacecmd "feidex/internal/adapter/feishu/workspacecmd"
	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/domain/conversation"
	"feidex/internal/state"
)

func workspaceStateDeps(store *appstate.Store) appworkspacecmd.StateDeps {
	return appworkspacecmd.StateDeps{
		GetSession: func(key string) *conversation.Session { return store.Session(key) },
		Sessions:   func() []*conversation.Session { return store.Sessions() },
		Pending:    func(id string) *state.PendingRequest { return store.Pending(id) },
	}
}
