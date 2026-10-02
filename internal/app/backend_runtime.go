package app

import (
	"context"
	appbackend "feidex/internal/app/backend"
	"feidex/internal/config"
	"feidex/internal/domain/conversation"
	"fmt"
	"strings"
	"time"
)

// backendRuntimeContext is the small set of capabilities a backend runtime
// needs from the frontend host. Keeping these capabilities explicit prevents
// backend facades from becoming a second App-shaped service boundary.
type backendRuntimeContext struct {
	backend    string
	frontendID string
	cfg        *config.Config
	codex      CodexClient
	claude     ClaudeCore

	beginStartupRecoveryScope      func() func()
	reconcileCompletedTurn         func(string, *conversation.Session) *conversation.Session
	reconcileClaudeCompletedTurn   func(string, *conversation.Session) *conversation.Session
	clearActiveOperations          func(string, *conversation.Session) *conversation.Session
	buildCodexClient               func() CodexClient
	configureCodexClient           func(CodexClient)
	newClaudeCore                  func() ClaudeCore
	startCodex                     func(context.Context, CodexClient) error
	codexMaintenanceActive         func() bool
	claudeMaintenanceActive        func() bool
	maintenanceBlocksCommand       func(string) error
	deferQueuedSubmissionsRecovery func() bool
	dropThreadLineageAfterFailure  func(error) bool
	handleTransportFailure         func(string, string, error)
}

func backendRuntimeContextForApp(a *App) backendRuntimeContext {
	if a == nil {
		return backendRuntimeContext{}
	}
	ctx := backendRuntimeContext{
		backend:    configuredBackend(a),
		frontendID: a.frontendID,
		cfg:        a.cfg,
		// Do not ask RecoveryService for its current client while recovery is
		// holding its mutex (startup recovery calls back into this context).
		codex:  getCodex(a),
		claude: currentClaudeCore(a),
	}
	ctx.beginStartupRecoveryScope = func() func() { return beginCodexAutoThreadRecoveryScope(a) }
	ctx.reconcileCompletedTurn = func(key string, sess *conversation.Session) *conversation.Session {
		return reconcileCompletedCodexTurnFromFinalOutput(a, key, sess)
	}
	ctx.reconcileClaudeCompletedTurn = func(key string, sess *conversation.Session) *conversation.Session {
		if sess == nil {
			return sess
		}
		finishTurn(a, strings.TrimSpace(sess.ActiveThreadID), strings.TrimSpace(sess.ActiveTurnID), "completed")
		return a.State().Session(key)
	}
	ctx.clearActiveOperations = func(key string, sess *conversation.Session) *conversation.Session {
		return clearClaudeActiveOperationsAfterInterrupt(a, key, sess)
	}
	ctx.buildCodexClient = func() CodexClient {
		if a.cfg == nil {
			return nil
		}
		return newCodexClient(a.cfg.Codex)
	}
	ctx.configureCodexClient = func(client CodexClient) { configureCodexClientRuntime(a, client) }
	ctx.newClaudeCore = func() ClaudeCore {
		if a.cfg == nil {
			return nil
		}
		return newClaudeCore(a, a.cfg.Claude)
	}
	ctx.startCodex = func(startCtx context.Context, client CodexClient) error {
		if client == nil || a.cfg == nil {
			return nil
		}
		return client.Start(startCtx, a.cfg.Codex.ExperimentalAPI)
	}
	ctx.codexMaintenanceActive = func() bool { return appbackend.NewMaintenanceStateService(a).CodexMaintenanceActive() }
	ctx.claudeMaintenanceActive = func() bool { return appbackend.NewMaintenanceStateService(a).ClaudeMaintenanceActive() }
	ctx.maintenanceBlocksCommand = func(raw string) error {
		if configuredBackend(a) == backendClaude {
			return appbackend.NewMaintenanceStateService(a).ClaudeMaintenanceBlocksCommand(raw)
		}
		return appbackend.NewMaintenanceStateService(a).CodexMaintenanceBlocksCommand(raw)
	}
	ctx.deferQueuedSubmissionsRecovery = func() bool { return codexRuntimeRecovering(a) }
	ctx.dropThreadLineageAfterFailure = func(err error) bool {
		if codexRuntimeRecovering(a) {
			return true
		}
		text := ""
		if err != nil {
			text = err.Error()
		}
		return strings.Contains(strings.ToLower(text), "codex client not initialized") ||
			strings.Contains(strings.ToLower(text), "codex app-server read failed") ||
			strings.Contains(strings.ToLower(text), "codex app-server stdin write failed") ||
			strings.Contains(strings.ToLower(text), "codex app-server process exited")
	}
	ctx.handleTransportFailure = func(sessionKey, threadID string, err error) {
		if configuredBackend(a) == backendClaude {
			failClaudeSessionActiveWork(a, sessionKey, threadID, err)
			return
		}
		failBackendActiveWork(a, backendCodex, sessionKey, threadID, errorText(err))
	}
	return ctx
}

type backendRuntimeHandle struct {
	backend string
	codex   CodexClient
	claude  ClaudeCore
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
		setCompositionClaude(a, nil)
		return
	}
	setRuntimeBackend(a, h.backend)
	replaceCodexClient(a, h.codex)
	setCompositionClaude(a, h.claude)
}

type backendRuntimeFacade interface {
	kind() string
	displayName() string
	configuredCommand(ctx backendRuntimeContext) string
	isActive(ctx backendRuntimeContext) bool
	runtimeReady(ctx backendRuntimeContext) bool
	beginStartupRecoveryScope(ctx backendRuntimeContext) func()
	reconcileCompletedTurnFromFinalOutput(ctx backendRuntimeContext, sessionKey string, sess *conversation.Session) *conversation.Session
	// clearActiveOperationsAfterInterrupt clears stale active operations after
	// an interrupt request. For backends where the interrupt response is
	// asynchronous (e.g. Claude), this prevents the session from getting stuck
	// in "queuing" state if the interrupt doesn't trigger a turn completion.
	clearActiveOperationsAfterInterruptContext(ctx backendRuntimeContext, sessionKey string, sess *conversation.Session) *conversation.Session
	buildRuntime(ctx backendRuntimeContext) *backendRuntimeHandle
	startRuntime(ctx context.Context, runtimeCtx backendRuntimeContext, handle *backendRuntimeHandle) error
	maintenanceActive(ctx backendRuntimeContext) bool
	maintenanceBlocksCommand(ctx backendRuntimeContext, raw string) error
	idleMaintenanceBlockedReason() string
	resolvesPendingLocally(kind string) bool
	deferQueuedSubmissionsDuringRecovery(ctx backendRuntimeContext) bool
	dropThreadLineageAfterStartFailure(ctx backendRuntimeContext, err error) bool
	failsStandaloneCompaction() bool
	handleTransportFailure(ctx backendRuntimeContext, sessionKey, threadID string, err error)
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
		claude:  currentClaudeCore(a),
	}
}

func buildBackendRuntimeHandle(a *App, target string) (*backendRuntimeHandle, error) {
	runtime := backendRuntimeForKind(target)
	if runtime == nil {
		return nil, fmt.Errorf("unsupported backend %q", target)
	}
	return runtime.buildRuntime(backendRuntimeContextForApp(a)), nil
}

func startPreparedBackendRuntime(a *App, ctx context.Context, handle *backendRuntimeHandle) error {
	if a == nil || handle == nil {
		return nil
	}
	runtime := backendRuntimeForKind(handle.backend)
	if runtime == nil {
		return nil
	}
	return runtime.startRuntime(ctx, backendRuntimeContextForApp(a), handle)
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
