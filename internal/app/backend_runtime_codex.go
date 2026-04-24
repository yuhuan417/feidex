package app

import (
	"context"
	"log/slog"
	"strings"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type codexRuntimeFacade struct{}

func (codexRuntimeFacade) kind() string { return backendCodex }

func (codexRuntimeFacade) displayName() string { return "Codex" }

func (codexRuntimeFacade) configuredCommand(a *App) string {
	if a == nil || a.cfg == nil {
		return ""
	}
	return strings.TrimSpace(a.cfg.Codex.Command)
}

func (codexRuntimeFacade) isActive(a *App) bool {
	return a != nil && a.configuredBackend() == backendCodex
}

func (codexRuntimeFacade) runtimeReady(a *App) bool {
	return a != nil && a.currentCodexClient() != nil
}

func (codexRuntimeFacade) beginStartupRecoveryScope(a *App) func() {
	if a == nil {
		return func() {}
	}
	return a.beginCodexAutoThreadRecoveryScope()
}

func (codexRuntimeFacade) reconcileCompletedTurnFromFinalOutput(a *App, sessionKey string, sess *state.Session) *state.Session {
	if a == nil {
		return sess
	}
	return a.reconcileCompletedCodexTurnFromFinalOutput(sessionKey, sess)
}

func (codexRuntimeFacade) conversationBackend(a *App) conversationBackendFacade {
	return codexConversationBackend{app: a}
}

func (codexRuntimeFacade) configuration(a *App) backendConfigurationFacade {
	return codexBackendConfigurationFacade{app: a}
}

func (codexRuntimeFacade) serverRequestAdapter(a *App) serverRequestBackendAdapter {
	return codexServerRequestAdapter{app: a}
}

func (codexRuntimeFacade) runMenuCompactAction(a *App, action *feishu.CardAction, sessionKey string) error {
	if a == nil {
		return nil
	}
	msg := a.commandMessageFromAction(action, sessionKey, "/compact")
	sessionKey = firstNonEmpty(a.makeSessionKey(msg), strings.TrimSpace(sessionKey))
	_, err := a.startThreadCompaction(sessionKey)
	return err
}

func (codexRuntimeFacade) handleCompactCommand(a *App, msg *feishu.InboundMessage) error {
	if a == nil || msg == nil {
		return nil
	}
	if _, err := a.startThreadCompaction(a.makeSessionKey(msg)); err != nil {
		return err
	}
	return a.feishu.ReplyText(context.Background(), msg.MessageID, "已请求压缩当前线程上下文。", a.replyInThreadEnabled(msg.ChatType))
}

func (codexRuntimeFacade) completeMenuInterrupt(a *App, action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	parentAction := actionStringValue(action, "parent_action")
	return a.completeMenuCommand(action, sessionKey, "/stop", parentAction)
}

func (codexRuntimeFacade) buildRuntime(a *App) *backendRuntimeHandle {
	if a == nil {
		return &backendRuntimeHandle{backend: backendCodex}
	}
	client := newCodexClient(a.cfg.Codex)
	a.configureCodexClientRuntime(client)
	return &backendRuntimeHandle{
		backend: backendCodex,
		codex:   client,
	}
}

func (codexRuntimeFacade) startRuntime(ctx context.Context, a *App, handle *backendRuntimeHandle) error {
	if a == nil || handle == nil || handle.codex == nil {
		return nil
	}
	return handle.codex.Start(ctx, a.cfg.Codex.ExperimentalAPI)
}

func (codexRuntimeFacade) maintenanceActive(a *App) bool {
	return a != nil && a.codexMaintenanceActive()
}

func (codexRuntimeFacade) maintenanceBlocksCommand(a *App, raw string) error {
	if a == nil {
		return nil
	}
	return a.codexMaintenanceBlocksCommand(raw)
}

func (codexRuntimeFacade) idleMaintenanceBlockedReason() string {
	return "当前正在执行 Codex 维护，请稍后再切换 backend"
}

func (codexRuntimeFacade) resolvesPendingLocally(kind string) bool {
	return !isServerResolvedPendingKind(kind)
}

func (codexRuntimeFacade) deferQueuedSubmissionsDuringRecovery(a *App) bool {
	return a != nil && a.codexRuntimeRecovering()
}

func (codexRuntimeFacade) dropThreadLineageAfterStartFailure(a *App, err error) bool {
	if a == nil || err == nil {
		return false
	}
	if a.codexRuntimeRecovering() {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(text, "codex client not initialized"):
		return true
	case strings.Contains(text, "codex app-server read failed"):
		return true
	case strings.Contains(text, "codex app-server stdin write failed"):
		return true
	case strings.Contains(text, "codex app-server process exited"):
		return true
	default:
		return false
	}
}

func (codexRuntimeFacade) failsStandaloneCompaction() bool {
	return true
}

func (codexRuntimeFacade) handleTransportFailure(a *App, _, _ string, err error) {
	if a == nil {
		return
	}
	message := "Codex 后端异常退出。"
	if detail := strings.TrimSpace(errorText(err)); detail != "" {
		message = "Codex 后端异常退出：" + detail
	}
	slog.Error("codex backend transport failed",
		"frontend_id", a.frontendID,
		"error", err,
	)
	a.runAsync(func() {
		a.failBackendActiveWork(backendCodex, "", "", message)
	})
}
