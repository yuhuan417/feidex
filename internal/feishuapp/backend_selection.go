package feishuapp

import (
	"context"
	backendruntime "feidex/internal/runtime"
	"os/exec"

	configadapter "feidex/internal/adapter/config"
	"feidex/internal/adapter/feishu/backend"
	"feidex/internal/application/backendselection"
	"feidex/internal/feishu"
)

// backendLookPath is testable indirection for exec.LookPath.
var backendLookPath = exec.LookPath

func buildBackendSelectionService(app *App) backend.SelectionService {
	if app == nil {
		return backend.SelectionService{}
	}
	announcementRefresh := app.runtimeOwner.Announcements
	announcementQuery := app.bindings.AnnouncementQuery
	startupRecovery := app.bindings.StartupRecovery
	autoRetry := app.bindings.AutoRetry

	return backend.NewSelectionService(backend.SelectionDeps{
		Source:  app,
		UseCase: app.bindings.BackendSwitch,
		Runtime: backend.SelectionRuntimeDeps{
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
				recoverFrontendRuntimeState(startupRecovery)
				scheduleAllGroupAnnouncementStatusRefreshes(announcementRefresh, announcementQuery)
			},
			IdleBlockedReason: func() string {
				return frontendIdleBlockedReason(app)
			},
			RuntimeReady: func(target string) bool {
				return backendRuntimeReadyForApp(app, target)
			},
		},
		Render: backend.SelectionRenderDeps{
			BuildStatusCard: func(title, color, body string, buttons []feishu.Button) map[string]any {
				return app.Feishu().SimpleStatusCard(title, color, body, buttons)
			},
			BuildMenuCard: func(sessionKey string) map[string]any {
				return renderBackendMenuCard(app, sessionKey)
			},
			BuildCardBody: func(action, body string) string {
				return menuCardBody(action, body)
			},
		},
		Effects: backend.SelectionEffectDeps{
			ReplyCard: func(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
				return replyCardWithIDEffect(ctx, app, messageID, card, inThread)
			},
			SendCard: func(ctx context.Context, chatID string, card map[string]any) (string, error) {
				return sendCardWithIDEffect(ctx, app, chatID, card)
			},
			PatchCard: func(ctx context.Context, messageID string, card map[string]any) error {
				return patchCardEffect(ctx, app, messageID, card)
			},
			RunAsync: func(sessionKey string, fn func()) { runSessionAsync(app, sessionKey, fn) },
		},
		Commands: backend.SelectionCommandDeps{
			CommandAutoRetry: func(msg *feishu.InboundMessage, args []string) error {
				return autoRetry.CommandAutoRetry(msg, args)
			},
		},
	})
}

type backendSelectionRuntime struct{ app *App }

func (r backendSelectionRuntime) AvailableBackends() []backendselection.AvailableBackend {
	return availableBackendsForApp(r.app)
}
func (r backendSelectionRuntime) Ready(target string) bool {
	return backendRuntimeReadyForApp(r.app, target)
}
func (r backendSelectionRuntime) IdleBlockedReason() string { return frontendIdleBlockedReason(r.app) }
func (r backendSelectionRuntime) Prepare(ctx context.Context, target string) (*backendselection.RuntimeHandle, error) {
	return prepareRuntimeForApp(r.app, ctx, target)
}
func (r backendSelectionRuntime) Snapshot() *backendselection.RuntimeHandle {
	return snapshotRuntimeForApp(r.app)
}
func (r backendSelectionRuntime) Recover() {
	recoverFrontendRuntimeState(r.app.bindings.StartupRecovery)
	scheduleAllGroupAnnouncementStatusRefreshes(r.app.runtimeOwner.Announcements, r.app.bindings.AnnouncementQuery)
}

func BackendSwitchPorts(a *App) backendselection.Dependencies {
	return backendselection.Dependencies{Repository: configadapter.BackendSelectionRepository{Source: a, Configured: func() string { return a.configView().configuredBackend() }}, Transition: &a.runtimeOwner.BackendTransition, Runtime: backendSelectionRuntime{app: a}}
}

func availableBackendsForApp(app *App) []backend.AvailableBackend {
	if app == nil || app.cfg == nil {
		return nil
	}
	out := make([]backend.AvailableBackend, 0, 2)
	for _, runtime := range backendruntime.Backends() {
		command := runtime.ConfiguredCommand(backendRuntimeContextForApp(app.BackendRuntimeDeps()))
		if command == "" {
			continue
		}
		path, err := backendLookPath(command)
		if err != nil {
			continue
		}
		out = append(out, backend.AvailableBackend{
			Kind:    runtime.Kind(),
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
		Close:   h.Close,
		Install: func() { installBackendRuntime(app, h) },
	}, nil
}

func snapshotRuntimeForApp(app *App) *backend.BackendRuntimeHandle {
	h := currentBackendRuntimeHandle(app)
	if h == nil {
		return nil
	}
	return &backend.BackendRuntimeHandle{
		Close:   h.Close,
		Install: func() { installBackendRuntime(app, h) },
	}
}

func backendRuntimeReadyForApp(app *App, target string) bool {
	if runtime := backendruntime.BackendForKind(target); runtime != nil {
		return runtime.RuntimeReady(backendRuntimeContextForApp(app.BackendRuntimeDeps()))
	}
	return false
}
