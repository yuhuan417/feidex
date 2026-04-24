package app

import (
	"context"
	"fmt"
	"time"

	"feidex/internal/state"
)

type backendRuntimeHandle struct {
	backend string
	codex   codexClient
	claude  claudeCore
}

func (h *backendRuntimeHandle) close() error {
	if h == nil {
		return nil
	}
	if h.claude != nil {
		_ = h.claude.Close()
	}
	if h.codex != nil {
		return h.codex.Close()
	}
	return nil
}

func (h *backendRuntimeHandle) install(a *App) {
	if a == nil {
		return
	}
	if h == nil {
		setRuntimeBackend(a, "")
		replaceCodexClient(a, nil)
		a.claude = nil
		return
	}
	setRuntimeBackend(a, h.backend)
	replaceCodexClient(a, h.codex)
	a.claude = h.claude
}

type backendRuntimeFacade interface {
	kind() string
	displayName() string
	configuredCommand(a *App) string
	isActive(a *App) bool
	runtimeReady(a *App) bool
	beginStartupRecoveryScope(a *App) func()
	reconcileCompletedTurnFromFinalOutput(a *App, sessionKey string, sess *state.Session) *state.Session
	conversationBackend(a *App) conversationBackendFacade
	configuration(a *App) backendConfigurationFacade
	serverRequestAdapter(a *App) serverRequestBackendAdapter
	buildRuntime(a *App) *backendRuntimeHandle
	startRuntime(ctx context.Context, a *App, handle *backendRuntimeHandle) error
	maintenanceActive(a *App) bool
	maintenanceBlocksCommand(a *App, raw string) error
	idleMaintenanceBlockedReason() string
	resolvesPendingLocally(kind string) bool
	deferQueuedSubmissionsDuringRecovery(a *App) bool
	dropThreadLineageAfterStartFailure(a *App, err error) bool
	failsStandaloneCompaction() bool
	handleTransportFailure(a *App, sessionKey, threadID string, err error)
}

func backendRuntimeForKind(kind string) backendRuntimeFacade {
	switch normalizeRuntimeBackend(kind) {
	case backendCodex:
		return codexRuntimeFacade{}
	case backendClaude:
		return claudeRuntimeFacade{}
	default:
		return nil
	}
}

func backendRuntimeFacades() []backendRuntimeFacade {
	return []backendRuntimeFacade{
		codexRuntimeFacade{},
		claudeRuntimeFacade{},
	}
}

func backendRuntime(a *App) backendRuntimeFacade {
	if a == nil {
		return nil
	}
	return backendRuntimeForKind(configuredBackend(a))
}

func currentBackendRuntimeHandle(a *App) *backendRuntimeHandle {
	if a == nil {
		return nil
	}
	return &backendRuntimeHandle{
		backend: configuredBackend(a),
		codex:   currentCodexClient(a),
		claude:  a.claude,
	}
}

func buildBackendRuntimeHandle(a *App, target string) (*backendRuntimeHandle, error) {
	runtime := backendRuntimeForKind(target)
	if runtime == nil {
		return nil, fmt.Errorf("unsupported backend %q", target)
	}
	return runtime.buildRuntime(a), nil
}

func startPreparedBackendRuntime(a *App, ctx context.Context, handle *backendRuntimeHandle) error {
	if a == nil || handle == nil {
		return nil
	}
	runtime := backendRuntimeForKind(handle.backend)
	if runtime == nil {
		return nil
	}
	return runtime.startRuntime(ctx, a, handle)
}

func prepareBackendRuntime(a *App, ctx context.Context, target string) (*backendRuntimeHandle, error) {
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
		_ = handle.close()
		return nil, err
	}
	return handle, nil
}
