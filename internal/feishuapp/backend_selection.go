package feishuapp

import (
	"context"
	backendruntime "feidex/internal/runtime"
	"os/exec"
	"sync"

	configadapter "feidex/internal/adapter/config"
	"feidex/internal/adapter/feishu/backend"
	"feidex/internal/application/announcement"
	"feidex/internal/application/backendselection"
	"feidex/internal/application/frontend"
	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/runtime/maintenance"
	"feidex/internal/state"
)

// backendLookPath is testable indirection for exec.LookPath.
var backendLookPath = exec.LookPath

func BuildBackendSelection(app *App) backend.SelectionService {
	if app == nil {
		return backend.SelectionService{}
	}
	announcementRefresh := app.runtimeOwner.Announcements
	announcementQuery := app.bindings.AnnouncementQuery
	startupRecovery := app.bindings.StartupRecovery
	autoRetry := app.bindings.AutoRetry
	frontendQuery := app.bindings.FrontendQuery
	effectRunner := newEffectRunner(app.runtimeOwner)
	frontendID := app.FrontendID()
	source := newFrontendConfigProvider(app.BackendRuntimeDeps(), app.store, app.bindings.WorkspaceSelection)

	return backend.NewSelectionService(backend.SelectionDeps{
		Source:  source,
		UseCase: app.bindings.BackendSwitch,
		Runtime: backend.SelectionRuntimeDeps{
			ListAvailableBackends: func() []backend.AvailableBackend {
				return availableBackendsForApp(app.BackendRuntimeDeps())
			},
			PrepareRuntime: func(ctx context.Context, target string) (*backend.BackendRuntimeHandle, error) {
				return prepareRuntimeForApp(app.BackendRuntimeDeps(), ctx, target)
			},
			SnapshotRuntime: func() *backend.BackendRuntimeHandle {
				return snapshotRuntimeForApp(app.BackendRuntimeDeps())
			},
			RecoverState: func() {
				recoverFrontendRuntimeState(startupRecovery)
				scheduleAllGroupAnnouncementStatusRefreshes(announcementRefresh, announcementQuery)
			},
			IdleBlockedReason: func() string {
				return frontendIdleBlockedReason(frontendQuery)
			},
			RuntimeReady: func(target string) bool {
				return backendRuntimeReadyForApp(app.BackendRuntimeDeps(), target)
			},
		},
		Render: backend.SelectionRenderDeps{
			BuildStatusCard: func(title, color, body string, buttons []feishu.Button) map[string]any {
				return app.Feishu().SimpleStatusCard(title, color, body, buttons)
			},
			BuildMenuCard: func(sessionKey string) map[string]any {
				spec, _ := menuGroupSpec("menu.group.backend")
				return renderBackendMenuCardData(app.configView().configuredBackend(), planModeTitleForSession(app.State(), app != nil, sessionKey, spec.Label), app.feishu, sessionKey)
			},
			BuildCardBody: func(action, body string) string {
				return menuCardBody(action, body)
			},
		},
		Effects: backend.SelectionEffectDeps{
			ReplyCard: func(ctx context.Context, messageID string, card map[string]any, inThread bool) (string, error) {
				return replyCardWithIDEffect(ctx, effectRunner, frontendID, messageID, card, inThread)
			},
			SendCard: func(ctx context.Context, chatID string, card map[string]any) (string, error) {
				return sendCardWithIDEffect(ctx, effectRunner, frontendID, chatID, card)
			},
			PatchCard: func(ctx context.Context, messageID string, card map[string]any) error {
				return patchCardEffect(ctx, effectRunner, frontendID, messageID, card)
			},
			RunAsync: func(sessionKey string, fn func()) {
				_ = runSessionAsync(&app.runtimeOwner.Lifecycle, app.asyncRunner, app.runtimeOwner.SessionActors, sessionKey, fn)
			},
		},
		Commands: backend.SelectionCommandDeps{
			CommandAutoRetry: func(msg *feishu.InboundMessage, args []string) error {
				return autoRetry.CommandAutoRetry(msg, args)
			},
		},
	})
}

type backendSelectionRuntime struct {
	runtimeDeps   BackendRuntimeDeps
	frontendQuery frontend.Query
	recoverState  func()
}

func (r backendSelectionRuntime) AvailableBackends() []backendselection.AvailableBackend {
	return availableBackendsForApp(r.runtimeDeps)
}
func (r backendSelectionRuntime) Ready(target string) bool {
	return backendRuntimeReadyForApp(r.runtimeDeps, target)
}
func (r backendSelectionRuntime) IdleBlockedReason() string {
	return frontendIdleBlockedReason(r.frontendQuery)
}
func (r backendSelectionRuntime) Prepare(ctx context.Context, target string) (*backendselection.RuntimeHandle, error) {
	return prepareRuntimeForApp(r.runtimeDeps, ctx, target)
}
func (r backendSelectionRuntime) Snapshot() *backendselection.RuntimeHandle {
	return snapshotRuntimeForApp(r.runtimeDeps)
}
func (r backendSelectionRuntime) Recover() {
	if r.recoverState != nil {
		r.recoverState()
	}
}

type BackendSwitchPortInputs struct {
	RuntimeDeps       BackendRuntimeDeps
	Transition        *backendruntime.BackendTransition
	FrontendQuery     frontend.Query
	StartupRecovery   maintenance.StartupRecovery
	Announcements     *backendruntime.CoalescedRefresh
	AnnouncementQuery announcement.Query
}

type backendSelectionSource struct{ deps BackendRuntimeDeps }

func (s backendSelectionSource) Config() *config.Config   { return s.deps.cfg }
func (s backendSelectionSource) ConfigMu() *sync.RWMutex  { return s.deps.view.mu }
func (s backendSelectionSource) ConfigPath() string       { return s.deps.cfgPath }
func (s backendSelectionSource) FrontendConfigIndex() int { return s.deps.view.frontendConfigIndex }
func (s backendSelectionSource) FrontendID() string       { return s.deps.frontendID }
func (s backendSelectionSource) Backend() string {
	if s.deps.runtime.owner == nil {
		return ""
	}
	return s.deps.runtime.owner.Backend()
}
func (s backendSelectionSource) Store() *state.Store { return s.deps.store }

func BackendSwitchPorts(inputs BackendSwitchPortInputs) backendselection.Dependencies {
	deps := inputs.RuntimeDeps
	return backendselection.Dependencies{
		Repository: configadapter.BackendSelectionRepository{
			Source: backendSelectionSource{deps: deps},
			Configured: func() string {
				return deps.currentBackend().view.configuredBackend()
			},
		},
		Transition: inputs.Transition,
		Runtime: backendSelectionRuntime{
			runtimeDeps: deps, frontendQuery: inputs.FrontendQuery,
			recoverState: func() {
				recoverFrontendRuntimeState(inputs.StartupRecovery)
				scheduleAllGroupAnnouncementStatusRefreshes(inputs.Announcements, inputs.AnnouncementQuery)
			},
		},
	}
}

func availableBackendsForApp(deps BackendRuntimeDeps) []backend.AvailableBackend {
	if deps.cfg == nil {
		return nil
	}
	out := make([]backend.AvailableBackend, 0, 2)
	for _, runtime := range backendruntime.Backends() {
		command := runtime.ConfiguredCommand(backendRuntimeContextForApp(deps.currentBackend()))
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

func prepareRuntimeForApp(deps BackendRuntimeDeps, ctx context.Context, target string) (*backend.BackendRuntimeHandle, error) {
	h, err := prepareBackendRuntime(deps, ctx, target)
	if err != nil {
		return nil, err
	}
	return &backend.BackendRuntimeHandle{
		Close:   h.Close,
		Install: func() { installBackendRuntime(deps, h) },
	}, nil
}

func snapshotRuntimeForApp(deps BackendRuntimeDeps) *backend.BackendRuntimeHandle {
	if deps.runtime.owner == nil {
		return nil
	}
	current := deps.currentBackend()
	h := currentBackendRuntimeHandle(current.view.configuredBackend(), current.runtime)
	if h == nil {
		return nil
	}
	return &backend.BackendRuntimeHandle{
		Close:   h.Close,
		Install: func() { installBackendRuntime(deps, h) },
	}
}

func backendRuntimeReadyForApp(deps BackendRuntimeDeps, target string) bool {
	if runtime := backendruntime.BackendForKind(target); runtime != nil {
		return runtime.RuntimeReady(backendRuntimeContextForApp(deps.currentBackend()))
	}
	return false
}
