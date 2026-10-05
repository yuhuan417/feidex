package feishuapp

import (
	"context"

	appstate "feidex/internal/adapter/storage/json/scoped"
	"feidex/internal/application"
	"feidex/internal/application/backendfailure"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/application/conversation"
	"feidex/internal/application/turn"
	"feidex/internal/config"
	frontendruntime "feidex/internal/runtime"
	appcodexruntime "feidex/internal/runtime/codex"
	"feidex/internal/state"
)

// BackendRuntimeDeps is what the backend runtime context and the Codex
// notification/request dispatch path read off the frontend: the config view,
// the two backend clients, the dispatcher, and the six services the context
// wires into it.
//
// It is exported so composition can obtain it from Frontend and hand it to the port
// factories. That is the point: the factories stop taking the aggregate, and
// the only place that still holds one is Frontend.BackendRuntimeDeps().
type BackendRuntimeDeps struct {
	view       frontendConfigView
	cfg        *config.Config
	cfgPath    string
	frontendID string
	runtime    runtimeView

	dispatcher        application.Dispatcher
	sessionActors     *frontendruntime.SessionActors
	contextFn         func() context.Context
	conversationQuery conversation.Query

	codexRecovery        appcodexruntime.RecoveryService
	turnReconciliation   *turn.Reconciliation
	claudeReconciliation *turn.StoppedReconciliation
	conversations        *conversation.Service
	maintenance          backendmaintenance.MaintenanceStateService
	backendFailure       *backendfailure.BackendFailureService
	mcp                  *feidexMCPService
	store                *state.Store
	stateView            *appstate.Store
	claudeFactory        func(config.ClaudeConfig) ClaudeCore
}

// BackendRuntimeDeps snapshots the bundle. It is the single conversion point:
// everything downstream takes BackendRuntimeDeps rather than *Frontend.
func (a *Frontend) BackendRuntimeDeps() BackendRuntimeDeps {
	if a == nil {
		return BackendRuntimeDeps{}
	}
	d := BackendRuntimeDeps{
		view:       a.configView(),
		cfg:        a.cfg,
		cfgPath:    a.cfgPath,
		frontendID: a.frontendID,
		runtime:    runtimeViewOf(a.runtimeOwner),
		contextFn:  a.Context,
	}
	if a.runtimeOwner != nil {
		if a.runtimeOwner.Dispatcher != nil {
			d.dispatcher = *a.runtimeOwner.Dispatcher
		}
		d.sessionActors = a.runtimeOwner.SessionActors
	}
	if a.bindings != nil {
		d.conversationQuery = a.bindings.ConversationQuery
		d.codexRecovery = a.bindings.CodexRecovery
		d.turnReconciliation = &a.bindings.TurnReconciliation
		d.claudeReconciliation = &a.bindings.ClaudeReconciliation
		d.conversations = a.bindings.Conversations
		d.maintenance = a.bindings.Maintenance
		d.backendFailure = a.bindings.BackendFailure
		d.mcp = a.bindings.MCP
		d.claudeFactory = a.bindings.ClaudeFactory
		d.store = a.store
		d.stateView = a.stateView
	}
	return d
}

func (d BackendRuntimeDeps) currentBackend() BackendRuntimeDeps {
	if d.runtime.owner != nil {
		d.view.backend = d.runtime.owner.Backend()
	}
	return d
}

func (d BackendRuntimeDeps) prepareClaudeMCPConfig(sessionKey string) (string, []string, func(), error) {
	return prepareClaudeMCPConfig(d.cfg, d.runtime.owner, d.mcp, sessionKey)
}
