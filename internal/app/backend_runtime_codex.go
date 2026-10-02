package app

import (
	"feidex/internal/domain/conversation"

	"context"
	"fmt"
	"log/slog"
	"strings"
)

type codexRuntimeFacade struct{}

func (codexRuntimeFacade) kind() string { return backendCodex }

func (codexRuntimeFacade) displayName() string { return "Codex" }

func (codexRuntimeFacade) configuredCommand(ctx backendRuntimeContext) string {
	if ctx.cfg == nil {
		return ""
	}
	return strings.TrimSpace(ctx.cfg.Codex.Command)
}

func (codexRuntimeFacade) isActive(ctx backendRuntimeContext) bool {
	return ctx.backend == backendCodex
}

func (codexRuntimeFacade) runtimeReady(ctx backendRuntimeContext) bool {
	return ctx.codex != nil
}

func (codexRuntimeFacade) beginStartupRecoveryScope(ctx backendRuntimeContext) func() {
	if ctx.beginStartupRecoveryScope == nil {
		return func() {}
	}
	return ctx.beginStartupRecoveryScope()
}

func (codexRuntimeFacade) reconcileCompletedTurnFromFinalOutput(ctx backendRuntimeContext, sessionKey string, sess *conversation.Session) *conversation.Session {
	if ctx.reconcileCompletedTurn == nil {
		return sess
	}
	return ctx.reconcileCompletedTurn(sessionKey, sess)
}

func (codexRuntimeFacade) clearActiveOperationsAfterInterruptContext(_ backendRuntimeContext, _ string, sess *conversation.Session) *conversation.Session {
	// Codex handles interrupt lifecycle asynchronously via turn/completed
	// notifications, so we don't clear active operations here.
	return sess
}

func (codexRuntimeFacade) buildRuntime(ctx backendRuntimeContext) *backendRuntimeHandle {
	if ctx.buildCodexClient == nil {
		return &backendRuntimeHandle{backend: backendCodex}
	}
	client := ctx.buildCodexClient()
	if ctx.configureCodexClient != nil {
		ctx.configureCodexClient(client)
	}
	return &backendRuntimeHandle{
		backend: backendCodex,
		codex:   client,
	}
}

func (codexRuntimeFacade) startRuntime(ctx context.Context, runtimeCtx backendRuntimeContext, handle *backendRuntimeHandle) error {
	if handle == nil || handle.codex == nil {
		return nil
	}
	if runtimeCtx.startCodex == nil {
		return nil
	}
	return runtimeCtx.startCodex(ctx, handle.codex)
}

func (codexRuntimeFacade) maintenanceActive(ctx backendRuntimeContext) bool {
	return ctx.codexMaintenanceActive != nil && ctx.codexMaintenanceActive()
}

func (codexRuntimeFacade) maintenanceBlocksCommand(ctx backendRuntimeContext, raw string) error {
	if ctx.maintenanceBlocksCommand == nil {
		return nil
	}
	return ctx.maintenanceBlocksCommand(raw)
}

func (codexRuntimeFacade) idleMaintenanceBlockedReason() string {
	return "当前正在执行 Codex 维护，请稍后再切换 backend"
}

func (codexRuntimeFacade) resolvesPendingLocally(kind string) bool {
	return !isServerResolvedPendingKind(kind)
}

func (codexRuntimeFacade) deferQueuedSubmissionsDuringRecovery(ctx backendRuntimeContext) bool {
	return ctx.deferQueuedSubmissionsRecovery != nil && ctx.deferQueuedSubmissionsRecovery()
}

func (codexRuntimeFacade) dropThreadLineageAfterStartFailure(ctx backendRuntimeContext, err error) bool {
	return err != nil && ctx.dropThreadLineageAfterFailure != nil && ctx.dropThreadLineageAfterFailure(err)
}

func (codexRuntimeFacade) failsStandaloneCompaction() bool {
	return true
}

func (codexRuntimeFacade) handleTransportFailure(ctx backendRuntimeContext, sessionKey, threadID string, err error) {
	if ctx.handleTransportFailure == nil {
		return
	}
	message := "Codex 后端异常退出。"
	if detail := strings.TrimSpace(errorText(err)); detail != "" {
		message = "Codex 后端异常退出：" + detail
	}
	slog.Error("codex backend transport failed",
		"frontend_id", ctx.frontendID,
		"error", err,
	)
	ctx.handleTransportFailure(sessionKey, threadID, fmt.Errorf("%s", message))
}
