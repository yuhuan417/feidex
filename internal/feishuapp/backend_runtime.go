package feishuapp

import (
	"context"
	"fmt"
	"time"

	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	backendruntime "feidex/internal/runtime"
)

func backendRuntimeContextForApp(a *App) backendruntime.BackendContext {
	if a == nil {
		return backendruntime.BackendContext{}
	}
	ctx := backendruntime.BackendContext{
		Backend:    configuredBackend(a),
		FrontendID: a.frontendID,
		Cfg:        a.cfg,
		// Do not ask RecoveryService for its current client while recovery is
		// holding its mutex (startup recovery calls back into this context).
		Codex:  getCodex(a),
		Claude: currentClaudeCore(a),
	}
	ctx.BeginStartupRecoveryScope = func() func() { return beginCodexAutoThreadRecoveryScope(a.bindings.CodexRecovery) }
	ctx.ReconcileCompletedTurn = func(key string, sess *conversation.Session) *conversation.Session {
		return reconcileCompletedCodexTurnFromFinalOutput(a.bindings.TurnReconciliation, key, sess)
	}
	ctx.ReconcileClaudeCompletedTurn = func(key string, sess *conversation.Session) *conversation.Session {
		return a.bindings.ClaudeReconciliation.Reconcile(key, sess)
	}
	ctx.ClearActiveOperations = func(key string, sess *conversation.Session) *conversation.Session {
		return a.bindings.Conversations.ClearInterruptedOperations(key, sess)
	}
	ctx.BuildCodexClient = func() CodexClient {
		if a.cfg == nil {
			return nil
		}
		return newCodexClient(a.cfg.Codex)
	}
	ctx.ConfigureCodexClient = func(client CodexClient) { configureCodexClientRuntime(a, client) }
	ctx.NewClaudeCore = func() ClaudeCore {
		if a.cfg == nil {
			return nil
		}
		return newClaudeCore(a, a.cfg.Claude)
	}
	ctx.StartCodex = func(startCtx context.Context, client CodexClient) error {
		if client == nil || a.cfg == nil {
			return nil
		}
		return client.Start(startCtx, a.cfg.Codex.ExperimentalAPI)
	}
	ctx.CodexMaintenanceActive = func() bool { return a.bindings.Maintenance.CodexMaintenanceActive() }
	ctx.ClaudeMaintenanceActive = func() bool { return a.bindings.Maintenance.ClaudeMaintenanceActive() }
	ctx.MaintenanceBlocksCommand = func(raw string) error {
		if configuredBackend(a) == domainbackend.BackendClaude {
			return a.bindings.Maintenance.ClaudeMaintenanceBlocksCommand(raw)
		}
		return a.bindings.Maintenance.CodexMaintenanceBlocksCommand(raw)
	}
	ctx.DeferQueuedSubmissionsRecovery = func() bool { return codexRuntimeRecovering(a.bindings.CodexRecovery) }
	ctx.DropThreadLineageAfterFailure = func(err error) bool {
		return domainbackend.DropCodexLineageAfterFailure(codexRuntimeRecovering(a.bindings.CodexRecovery), errorText(err))
	}
	ctx.HandleTransportFailure = func(sessionKey, threadID string, err error) {
		if configuredBackend(a) == domainbackend.BackendClaude {
			failClaudeSessionActiveWork(a.bindings.BackendFailure, sessionKey, threadID, err)
			return
		}
		failBackendActiveWork(a, domainbackend.BackendCodex, sessionKey, threadID, errorText(err))
	}
	return ctx
}

func backendRuntime(a *App) backendruntime.BackendFacade {
	if a == nil {
		return nil
	}
	return backendruntime.BackendForKind(configuredBackend(a))
}

func currentBackendRuntimeHandle(a *App) *backendruntime.BackendHandle {
	if a == nil {
		return nil
	}
	return &backendruntime.BackendHandle{
		Backend: configuredBackend(a), Codex: currentCodexClient(a), Claude: currentClaudeCore(a),
	}
}

func installBackendRuntime(a *App, h *backendruntime.BackendHandle) {
	if a == nil {
		return
	}
	if h == nil {
		setRuntimeBackend(a, "")
		replaceCodexClient(a.bindings.CodexRecovery, nil)
		setClaudeCore(a, nil)
		return
	}
	setRuntimeBackend(a, h.Backend)
	replaceCodexClient(a.bindings.CodexRecovery, h.Codex)
	setClaudeCore(a, h.Claude)
}

func buildBackendRuntimeHandle(a *App, target string) (*backendruntime.BackendHandle, error) {
	backend := backendruntime.BackendForKind(target)
	if backend == nil {
		return nil, fmt.Errorf("unsupported backend %q", target)
	}
	return backend.BuildRuntime(backendRuntimeContextForApp(a)), nil
}

func startPreparedBackendRuntime(a *App, ctx context.Context, handle *backendruntime.BackendHandle) error {
	if a == nil || handle == nil {
		return nil
	}
	backend := backendruntime.BackendForKind(handle.Backend)
	if backend == nil {
		return nil
	}
	return backend.StartRuntime(ctx, backendRuntimeContextForApp(a), handle)
}

func prepareBackendRuntime(a *App, ctx context.Context, target string) (*backendruntime.BackendHandle, error) {
	handle, err := buildBackendRuntimeHandle(a, target)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := startPreparedBackendRuntime(a, startCtx, handle); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return handle, nil
}
