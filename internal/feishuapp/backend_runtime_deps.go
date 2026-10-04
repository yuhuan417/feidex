package feishuapp

import (
	"context"

	"feidex/internal/application"
	"feidex/internal/application/backendfailure"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/application/conversation"
	"feidex/internal/application/turn"
	"feidex/internal/config"
	frontendruntime "feidex/internal/runtime"
	appcodexruntime "feidex/internal/runtime/codex"
)

// BackendRuntimeDeps is what the backend runtime context and the Codex
// notification/request dispatch path read off the frontend: the config view,
// the two backend clients, the dispatcher, and the six services the context
// wires into it.
//
// It is exported so composition can obtain it from App and hand it to the port
// factories. That is the point: the factories stop taking the aggregate, and
// the only place that still holds one is App.BackendRuntimeDeps().
type BackendRuntimeDeps struct {
	view       frontendConfigView
	cfg        *config.Config
	frontendID string
	runtime    runtimeView

	dispatcher        application.Dispatcher
	sessionActors     *frontendruntime.SessionActors
	contextFn         func() context.Context
	conversationQuery conversation.Query

	codexRecovery        appcodexruntime.RecoveryService
	turnReconciliation   turn.Reconciliation
	claudeReconciliation turn.StoppedReconciliation
	conversations        *conversation.Service
	maintenance          backendmaintenance.MaintenanceStateService
	backendFailure       *backendfailure.BackendFailureService
	mcp                  *feidexMCPService
}

// BackendRuntimeDeps snapshots the bundle. It is the single conversion point:
// everything downstream takes BackendRuntimeDeps rather than *App.
func (a *App) BackendRuntimeDeps() BackendRuntimeDeps {
	if a == nil {
		return BackendRuntimeDeps{}
	}
	d := BackendRuntimeDeps{
		view:       a.configView(),
		cfg:        a.cfg,
		frontendID: a.frontendID,
		runtime:    a.runtimeView(),
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
		d.turnReconciliation = a.bindings.TurnReconciliation
		d.claudeReconciliation = a.bindings.ClaudeReconciliation
		d.conversations = a.bindings.Conversations
		d.maintenance = a.bindings.Maintenance
		d.backendFailure = a.bindings.BackendFailure
		d.mcp = a.bindings.MCP
	}
	return d
}
