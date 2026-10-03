package runtime

import (
	"context"
	"feidex/internal/config"
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
)

type BackendContext struct {
	Backend    string
	FrontendID string
	Cfg        *config.Config
	Codex      CodexClient
	Claude     ClaudeCore

	BeginStartupRecoveryScope      func() func()
	ReconcileCompletedTurn         func(string, *conversation.Session) *conversation.Session
	ReconcileClaudeCompletedTurn   func(string, *conversation.Session) *conversation.Session
	ClearActiveOperations          func(string, *conversation.Session) *conversation.Session
	BuildCodexClient               func() CodexClient
	ConfigureCodexClient           func(CodexClient)
	NewClaudeCore                  func() ClaudeCore
	StartCodex                     func(context.Context, CodexClient) error
	CodexMaintenanceActive         func() bool
	ClaudeMaintenanceActive        func() bool
	MaintenanceBlocksCommand       func(string) error
	DeferQueuedSubmissionsRecovery func() bool
	DropThreadLineageAfterFailure  func(error) bool
	HandleTransportFailure         func(string, string, error)
}

type BackendHandle struct {
	Backend string
	Codex   CodexClient
	Claude  ClaudeCore
}

func (h *BackendHandle) Close() error {
	if h == nil {
		return nil
	}
	if h.Claude != nil {
		_ = h.Claude.Close()
	}
	if h.Codex != nil {
		return h.Codex.Close()
	}
	return nil
}

type BackendFacade interface {
	Kind() string
	DisplayName() string
	ConfiguredCommand(ctx BackendContext) string
	IsActive(ctx BackendContext) bool
	RuntimeReady(ctx BackendContext) bool
	BeginStartupRecoveryScope(ctx BackendContext) func()
	ReconcileCompletedTurnFromFinalOutput(ctx BackendContext, sessionKey string, sess *conversation.Session) *conversation.Session
	// clearActiveOperationsAfterInterrupt clears stale active operations after
	// an interrupt request. For backends where the interrupt response is
	// asynchronous (e.g. Claude), this prevents the session from getting stuck
	// in "queuing" state if the interrupt doesn't trigger a turn completion.
	ClearActiveOperationsAfterInterruptContext(ctx BackendContext, sessionKey string, sess *conversation.Session) *conversation.Session
	BuildRuntime(ctx BackendContext) *BackendHandle
	StartRuntime(ctx context.Context, runtimeCtx BackendContext, handle *BackendHandle) error
	MaintenanceActive(ctx BackendContext) bool
	MaintenanceBlocksCommand(ctx BackendContext, raw string) error
	IdleMaintenanceBlockedReason() string
	ResolvesPendingLocally(kind string) bool
	DeferQueuedSubmissionsDuringRecovery(ctx BackendContext) bool
	DropThreadLineageAfterStartFailure(ctx BackendContext, err error) bool
	FailsStandaloneCompaction() bool
	HandleTransportFailure(ctx BackendContext, sessionKey, threadID string, err error)
}

func BackendForKind(kind string) BackendFacade {
	switch domainbackend.NormalizeBackend(kind) {
	case domainbackend.BackendCodex:
		return codexRuntimeFacade{}
	case domainbackend.BackendClaude:
		return claudeRuntimeFacade{}
	default:
		return nil
	}
}

func Backends() []BackendFacade {
	return []BackendFacade{
		codexRuntimeFacade{},
		claudeRuntimeFacade{},
	}
}

func backendErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
