package app

import (
	"context"
	"os/exec"

	"feidex/internal/app/backend"
	"feidex/internal/feishu"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// backendLookPath is testable indirection for exec.LookPath.
var backendLookPath = exec.LookPath

// backendSelectionService wraps backend.SelectionService to preserve the
// lowercase method names used throughout app/. Callbacks that need deep
// app-package knowledge are injected here.
type backendSelectionService struct {
	app   *App
	inner backend.SelectionService
}

func newBackendSelectionService(app *App) backendSelectionService {
	s := backendSelectionService{app: app}
	s.inner = backend.SelectionService{
		App: app,
		ListAvailableBackends: func() []backend.AvailableBackend {
			return availableBackendsForApp(app)
		},
		PrepareRuntime: func(ctx context.Context, target string) (*backend.BackendRuntimeHandle, error) {
			return prepareRuntimeForApp(app, ctx, target)
		},
		SnapshotRuntime: func() *backend.BackendRuntimeHandle {
			return snapshotRuntimeForApp(app)
		},
		RecoverState: func() {
			recoverFrontendRuntimeState(app)
		},
		IdleBlockedReason: func() string {
			return frontendIdleBlockedReason(app)
		},
		RuntimeReady: func(target string) bool {
			return backendRuntimeReadyForApp(app, target)
		},
		BuildMenuCard: func(sessionKey string) map[string]any {
			return renderBackendMenuCard(app, sessionKey)
		},
		BuildCardBody: func(action, body string) string {
			return menuCardBody(action, body)
		},
		CommandAutoRetry: func(msg *feishu.InboundMessage, args []string) error {
			return newAutoRetryService(app).CommandAutoRetry(msg, args)
		},
	}
	return s
}

// --- Callback implementations ---

func availableBackendsForApp(app *App) []backend.AvailableBackend {
	if app == nil || app.cfg == nil {
		return nil
	}
	out := make([]backend.AvailableBackend, 0, 2)
	for _, runtime := range backendRuntimeFacades() {
		command := runtime.configuredCommand(app)
		if command == "" {
			continue
		}
		path, err := backendLookPath(command)
		if err != nil {
			continue
		}
		out = append(out, backend.AvailableBackend{
			Kind:    runtime.kind(),
			Command: command,
			Path:    path,
		})
	}
	return out
}

func prepareRuntimeForApp(app *App, ctx context.Context, target string) (*backend.BackendRuntimeHandle, error) {
	h, err := prepareBackendRuntime(app, ctx, target)
	if err != nil {
		return nil, err
	}
	return &backend.BackendRuntimeHandle{
		Close:   h.close,
		Install: func() { h.install(app) },
	}, nil
}

func snapshotRuntimeForApp(app *App) *backend.BackendRuntimeHandle {
	h := currentBackendRuntimeHandle(app)
	if h == nil {
		return nil
	}
	return &backend.BackendRuntimeHandle{
		Close:   h.close,
		Install: func() { h.install(app) },
	}
}

func backendRuntimeReadyForApp(app *App, target string) bool {
	if runtime := backendRuntimeForKind(target); runtime != nil {
		return runtime.runtimeReady(app)
	}
	return false
}

// --- Delegation methods (lowercase, preserving app package API) ---

func (s backendSelectionService) availableBackends() []backend.AvailableBackend {
	return s.inner.AvailableBackends()
}

func (s backendSelectionService) backendAvailable(target string) bool {
	return s.inner.BackendAvailable(target)
}

func (s backendSelectionService) renderBackendSelectionCard(sessionKey, notice string) map[string]any {
	return s.inner.RenderBackendSelectionCard(sessionKey, notice)
}

func (s backendSelectionService) renderBackendSwitchingCard(sessionKey, target string) map[string]any {
	return s.inner.RenderBackendSwitchingCard(sessionKey, target)
}

func (s backendSelectionService) replyBackendSelectionCard(msg *feishu.InboundMessage, reason string) error {
	return s.inner.ReplyBackendSelectionCard(msg, reason)
}

func (s backendSelectionService) commandBackend(msg *feishu.InboundMessage, args []string) error {
	return s.inner.CommandBackend(msg, args)
}

func (s backendSelectionService) completeMenuBackend(action *feishu.CardAction, sessionKey string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteMenuBackend(action, sessionKey)
}

func (s backendSelectionService) completeBackendSelect(action *feishu.CardAction, sessionKey, target string) (*callback.CardActionTriggerResponse, error) {
	return s.inner.CompleteBackendSelect(action, sessionKey, target)
}

func (s backendSelectionService) backendRuntimeReady(target string) bool {
	return s.inner.BackendRuntimeReady(target)
}

func (s backendSelectionService) backendSwitchBlockedReason() string {
	return s.inner.BackendSwitchBlockedReason()
}

func (s backendSelectionService) switchBackend(ctx context.Context, target string) error {
	return s.inner.SwitchBackend(ctx, target)
}

func (s backendSelectionService) setConfiguredBackend(target string) error {
	return s.inner.SetConfiguredBackend(target)
}
