package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
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
	runtimeReady(a *App) bool
	buildRuntime(a *App) *backendRuntimeHandle
	startRuntime(ctx context.Context, a *App, handle *backendRuntimeHandle) error
	maintenanceActive(a *App) bool
	maintenanceBlocksCommand(a *App, raw string) error
	idleMaintenanceBlockedReason() string
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

func (codexRuntimeFacade) runtimeReady(a *App) bool {
	return a != nil && a.currentCodexClient() != nil
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

func (claudeRuntimeFacade) runtimeReady(a *App) bool {
	return a != nil && a.claude != nil
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
