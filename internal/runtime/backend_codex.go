package runtime

import (
	domainbackend "feidex/internal/domain/backend"
	"feidex/internal/domain/conversation"
	"feidex/internal/domain/interaction"

	"context"
	"fmt"
	"log/slog"
	"strings"
)

type codexRuntimeFacade struct{}

func (codexRuntimeFacade) Kind() string { return domainbackend.BackendCodex }

func (codexRuntimeFacade) DisplayName() string { return "Codex" }

func (codexRuntimeFacade) ConfiguredCommand(ctx BackendContext) string {
	if ctx.Cfg == nil {
		return ""
	}
	return strings.TrimSpace(ctx.Cfg.Codex.Command)
}

func (codexRuntimeFacade) IsActive(ctx BackendContext) bool {
	return ctx.Backend == domainbackend.BackendCodex
}

func (codexRuntimeFacade) RuntimeReady(ctx BackendContext) bool {
	return ctx.Codex != nil
}

func (codexRuntimeFacade) BeginStartupRecoveryScope(ctx BackendContext) func() {
	if ctx.BeginStartupRecoveryScope == nil {
		return func() {}
	}
	return ctx.BeginStartupRecoveryScope()
}

func (codexRuntimeFacade) ReconcileCompletedTurnFromFinalOutput(ctx BackendContext, sessionKey string, sess *conversation.Session) *conversation.Session {
	if ctx.ReconcileCompletedTurn == nil {
		return sess
	}
	return ctx.ReconcileCompletedTurn(sessionKey, sess)
}

func (codexRuntimeFacade) ClearActiveOperationsAfterInterruptContext(_ BackendContext, _ string, sess *conversation.Session) *conversation.Session {
	// Codex handles interrupt lifecycle asynchronously via turn/completed
	// notifications, so we don't clear active operations here.
	return sess
}

func (codexRuntimeFacade) BuildRuntime(ctx BackendContext) *BackendHandle {
	if ctx.BuildCodexClient == nil {
		return &BackendHandle{Backend: domainbackend.BackendCodex}
	}
	client := ctx.BuildCodexClient()
	if ctx.ConfigureCodexClient != nil {
		ctx.ConfigureCodexClient(client)
	}
	return &BackendHandle{
		Backend: domainbackend.BackendCodex,
		Codex:   client,
	}
}

func (codexRuntimeFacade) StartRuntime(ctx context.Context, runtimeCtx BackendContext, handle *BackendHandle) error {
	if handle == nil || handle.Codex == nil {
		return nil
	}
	if runtimeCtx.StartCodex == nil {
		return nil
	}
	return runtimeCtx.StartCodex(ctx, handle.Codex)
}

func (codexRuntimeFacade) MaintenanceActive(ctx BackendContext) bool {
	return ctx.CodexMaintenanceActive != nil && ctx.CodexMaintenanceActive()
}

func (codexRuntimeFacade) MaintenanceBlocksCommand(ctx BackendContext, raw string) error {
	if ctx.MaintenanceBlocksCommand == nil {
		return nil
	}
	return ctx.MaintenanceBlocksCommand(raw)
}

func (codexRuntimeFacade) IdleMaintenanceBlockedReason() string {
	return "当前正在执行 Codex 维护，请稍后再切换 backend"
}

func (codexRuntimeFacade) ResolvesPendingLocally(kind string) bool {
	return !interaction.IsServerResolvedPendingKind(kind)
}

func (codexRuntimeFacade) DeferQueuedSubmissionsDuringRecovery(ctx BackendContext) bool {
	return ctx.DeferQueuedSubmissionsRecovery != nil && ctx.DeferQueuedSubmissionsRecovery()
}

func (codexRuntimeFacade) DropThreadLineageAfterStartFailure(ctx BackendContext, err error) bool {
	return err != nil && ctx.DropThreadLineageAfterFailure != nil && ctx.DropThreadLineageAfterFailure(err)
}

func (codexRuntimeFacade) FailsStandaloneCompaction() bool {
	return true
}

func (codexRuntimeFacade) HandleTransportFailure(ctx BackendContext, sessionKey, threadID string, err error) {
	if ctx.HandleTransportFailure == nil {
		return
	}
	message := "Codex 后端异常退出。"
	if detail := strings.TrimSpace(backendErrorText(err)); detail != "" {
		message = "Codex 后端异常退出：" + detail
	}
	slog.Error("codex backend transport failed",
		"frontend_id", ctx.FrontendID,
		"error", err,
	)
	ctx.HandleTransportFailure(sessionKey, threadID, fmt.Errorf("%s", message))
}
