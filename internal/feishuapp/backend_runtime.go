package feishuapp

import (
	"context"
	"fmt"
	"time"

	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	backendruntime "feidex/internal/runtime"
)

func backendRuntimeContextForApp(d BackendRuntimeDeps) backendruntime.BackendContext {
	ctx := backendruntime.BackendContext{
		Backend:    d.view.configuredBackend(),
		FrontendID: d.frontendID,
		Cfg:        d.cfg,
		// Do not ask RecoveryService for its current client while recovery is
		// holding its mutex (startup recovery calls back into this context).
		Codex:  d.runtime.getCodex(),
		Claude: d.runtime.currentClaudeCore(),
	}
	ctx.BeginStartupRecoveryScope = func() func() { return beginCodexAutoThreadRecoveryScope(d.codexRecovery) }
	ctx.ReconcileCompletedTurn = func(key string, sess *conversation.Session) *conversation.Session {
		if d.turnReconciliation == nil {
			return sess
		}
		return reconcileCompletedCodexTurnFromFinalOutput(*d.turnReconciliation, key, sess)
	}
	ctx.ReconcileClaudeCompletedTurn = func(key string, sess *conversation.Session) *conversation.Session {
		if d.claudeReconciliation == nil {
			return sess
		}
		return d.claudeReconciliation.Reconcile(key, sess)
	}
	ctx.ClearActiveOperations = func(key string, sess *conversation.Session) *conversation.Session {
		return d.conversations.ClearInterruptedOperations(key, sess)
	}
	ctx.BuildCodexClient = func() CodexClient {
		if d.cfg == nil {
			return nil
		}
		return newCodexClient(d.cfg.Codex)
	}
	ctx.ConfigureCodexClient = func(client CodexClient) { configureCodexClientRuntime(d, client) }
	ctx.NewClaudeCore = func() ClaudeCore {
		if d.cfg == nil {
			return nil
		}
		return newClaudeCore(d.claudeFactory, d.cfg.Claude)
	}
	ctx.StartCodex = func(startCtx context.Context, client CodexClient) error {
		if client == nil || d.cfg == nil {
			return nil
		}
		return client.Start(startCtx, d.cfg.Codex.ExperimentalAPI)
	}
	ctx.CodexMaintenanceActive = func() bool { return d.maintenance.CodexMaintenanceActive() }
	ctx.ClaudeMaintenanceActive = func() bool { return d.maintenance.ClaudeMaintenanceActive() }
	ctx.MaintenanceBlocksCommand = func(raw string) error {
		if d.view.configuredBackend() == domainbackend.BackendClaude {
			return d.maintenance.ClaudeMaintenanceBlocksCommand(raw)
		}
		return d.maintenance.CodexMaintenanceBlocksCommand(raw)
	}
	ctx.DeferQueuedSubmissionsRecovery = func() bool { return codexRuntimeRecovering(d.codexRecovery) }
	ctx.DropThreadLineageAfterFailure = func(err error) bool {
		return domainbackend.DropCodexLineageAfterFailure(codexRuntimeRecovering(d.codexRecovery), errorText(err))
	}
	ctx.HandleTransportFailure = func(sessionKey, threadID string, err error) {
		if d.view.configuredBackend() == domainbackend.BackendClaude {
			failClaudeSessionActiveWork(d.backendFailure, sessionKey, threadID, err)
			return
		}
		failBackendActiveWork(d, domainbackend.BackendCodex, sessionKey, threadID, errorText(err))
	}
	return ctx
}

func backendRuntime(a *App) backendruntime.BackendFacade {
	if a == nil {
		return nil
	}
	return backendruntime.BackendForKind(a.configView().configuredBackend())
}

func currentBackendRuntimeHandle(a *App) *backendruntime.BackendHandle {
	if a == nil {
		return nil
	}
	return &backendruntime.BackendHandle{
		Backend: a.configView().configuredBackend(), Codex: a.runtimeView().currentCodexClient(), Claude: a.runtimeView().currentClaudeCore(),
	}
}

func installBackendRuntime(a *App, h *backendruntime.BackendHandle) {
	if a == nil {
		return
	}
	if h == nil {
		setRuntimeBackend(a, "")
		replaceCodexClient(a.bindings.CodexRecovery, nil)
		a.runtimeView().setClaudeCore(nil)
		return
	}
	setRuntimeBackend(a, h.Backend)
	replaceCodexClient(a.bindings.CodexRecovery, h.Codex)
	a.runtimeView().setClaudeCore(h.Claude)
}

func buildBackendRuntimeHandle(a *App, target string) (*backendruntime.BackendHandle, error) {
	backend := backendruntime.BackendForKind(target)
	if backend == nil {
		return nil, fmt.Errorf("unsupported backend %q", target)
	}
	return backend.BuildRuntime(backendRuntimeContextForApp(a.BackendRuntimeDeps())), nil
}

func startPreparedBackendRuntime(a *App, ctx context.Context, handle *backendruntime.BackendHandle) error {
	if a == nil || handle == nil {
		return nil
	}
	backend := backendruntime.BackendForKind(handle.Backend)
	if backend == nil {
		return nil
	}
	return backend.StartRuntime(ctx, backendRuntimeContextForApp(a.BackendRuntimeDeps()), handle)
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
