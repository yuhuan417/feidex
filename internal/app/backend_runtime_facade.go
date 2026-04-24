package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"feidex/internal/feishu"
	"feidex/internal/state"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
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
		a.setRuntimeBackend("")
		a.replaceCodexClient(nil)
		a.claude = nil
		return
	}
	a.setRuntimeBackend(h.backend)
	a.replaceCodexClient(h.codex)
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
	runMenuCompactAction(a *App, action *feishu.CardAction, sessionKey string) error
	handleCompactCommand(a *App, msg *feishu.InboundMessage) error
	completeMenuInterrupt(a *App, action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error)
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

func (a *App) backendRuntime() backendRuntimeFacade {
	if a == nil {
		return nil
	}
	return backendRuntimeForKind(a.configuredBackend())
}

func (a *App) currentBackendRuntimeHandle() *backendRuntimeHandle {
	if a == nil {
		return nil
	}
	return &backendRuntimeHandle{
		backend: a.configuredBackend(),
		codex:   a.currentCodexClient(),
		claude:  a.claude,
	}
}

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

type claudeRuntimeFacade struct{}

func (claudeRuntimeFacade) kind() string { return backendClaude }

func (claudeRuntimeFacade) displayName() string { return "Claude" }

func (claudeRuntimeFacade) configuredCommand(a *App) string {
	if a == nil || a.cfg == nil {
		return ""
	}
	return strings.TrimSpace(a.cfg.Claude.Command)
}

func (claudeRuntimeFacade) isActive(a *App) bool {
	return a != nil && a.configuredBackend() == backendClaude
}

func (claudeRuntimeFacade) runtimeReady(a *App) bool {
	return a != nil && a.claude != nil
}

func (claudeRuntimeFacade) beginStartupRecoveryScope(*App) func() {
	return func() {}
}

func (claudeRuntimeFacade) reconcileCompletedTurnFromFinalOutput(_ *App, _ string, sess *state.Session) *state.Session {
	return sess
}

func (claudeRuntimeFacade) conversationBackend(a *App) conversationBackendFacade {
	return claudeConversationBackend{app: a}
}

func (claudeRuntimeFacade) configuration(a *App) backendConfigurationFacade {
	return claudeBackendConfigurationFacade{app: a}
}

func (claudeRuntimeFacade) serverRequestAdapter(a *App) serverRequestBackendAdapter {
	return claudeServerRequestAdapter{app: a}
}

func (claudeRuntimeFacade) runMenuCompactAction(a *App, action *feishu.CardAction, sessionKey string) error {
	if a == nil {
		return nil
	}
	msg := a.commandMessageFromAction(action, sessionKey, "/compact")
	return a.enqueueSubmission(msg)
}

func (claudeRuntimeFacade) handleCompactCommand(a *App, msg *feishu.InboundMessage) error {
	if a == nil || msg == nil {
		return nil
	}
	return a.enqueuePassthroughCommand(msg, "/compact")
}

func (claudeRuntimeFacade) completeMenuInterrupt(a *App, action *feishu.CardAction, sessionKey, targetTurnID string) (*callback.CardActionTriggerResponse, error) {
	parentAction := actionStringValue(action, "parent_action")
	return a.completeAsyncCommandAction(
		action,
		sessionKey,
		"/stop",
		parentAction,
		"正在请求中断当前任务",
		a.renderInterruptPreparingCard(sessionKey, parentAction),
		func(sessionKey, text string) map[string]any {
			return a.renderInterruptResultCard(sessionKey, parentAction, text)
		},
		func(sessionKey, errText string) map[string]any {
			return a.renderInterruptFailedCard(sessionKey, parentAction, targetTurnID, errText)
		},
		"interrupt patch failed",
	)
}

func (claudeRuntimeFacade) buildRuntime(a *App) *backendRuntimeHandle {
	if a == nil {
		return &backendRuntimeHandle{backend: backendClaude}
	}
	return &backendRuntimeHandle{
		backend: backendClaude,
		claude:  newClaudeCore(a, a.cfg.Claude),
	}
}

func (claudeRuntimeFacade) startRuntime(context.Context, *App, *backendRuntimeHandle) error {
	return nil
}

func (claudeRuntimeFacade) maintenanceActive(a *App) bool {
	return a != nil && a.claudeMaintenanceActive()
}

func (claudeRuntimeFacade) maintenanceBlocksCommand(a *App, raw string) error {
	if a == nil {
		return nil
	}
	return a.claudeMaintenanceBlocksCommand(raw)
}

func (claudeRuntimeFacade) idleMaintenanceBlockedReason() string {
	return "当前正在执行 Claude 维护，请稍后再切换 backend"
}

func (claudeRuntimeFacade) resolvesPendingLocally(string) bool {
	return true
}

func (claudeRuntimeFacade) deferQueuedSubmissionsDuringRecovery(*App) bool {
	return false
}

func (claudeRuntimeFacade) dropThreadLineageAfterStartFailure(*App, error) bool {
	return false
}

func (claudeRuntimeFacade) failsStandaloneCompaction() bool {
	return false
}

func (claudeRuntimeFacade) handleTransportFailure(a *App, sessionKey, threadID string, err error) {
	if a == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	threadID = strings.TrimSpace(threadID)
	if sessionKey == "" && threadID == "" {
		return
	}
	message := "Claude 会话异常结束。"
	if detail := strings.TrimSpace(errorText(err)); detail != "" {
		message = "Claude 会话异常结束：" + detail
	}
	slog.Warn("claude session failed",
		"session_key", sessionKey,
		"thread_id", threadID,
		"error", err,
	)
	a.failBackendActiveWork(backendClaude, sessionKey, threadID, message)
}

func (a *App) buildBackendRuntimeHandle(target string) (*backendRuntimeHandle, error) {
	runtime := backendRuntimeForKind(target)
	if runtime == nil {
		return nil, fmt.Errorf("unsupported backend %q", target)
	}
	return runtime.buildRuntime(a), nil
}

func (a *App) startPreparedBackendRuntime(ctx context.Context, handle *backendRuntimeHandle) error {
	if a == nil || handle == nil {
		return nil
	}
	runtime := backendRuntimeForKind(handle.backend)
	if runtime == nil {
		return nil
	}
	return runtime.startRuntime(ctx, a, handle)
}

func (a *App) prepareBackendRuntime(ctx context.Context, target string) (*backendRuntimeHandle, error) {
	handle, err := a.buildBackendRuntimeHandle(target)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := a.startPreparedBackendRuntime(startCtx, handle); err != nil {
		_ = handle.close()
		return nil, err
	}
	return handle, nil
}
