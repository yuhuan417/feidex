package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/upgraderender"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/config"
	frontendruntime "feidex/internal/runtime"
	clauderuntime "feidex/internal/runtime/claude"
	"strings"
	"sync"
)

type backendMaintenanceRuntime struct {
	app  *App
	kind string
}

func (p backendMaintenanceRuntime) Refresh(ctx context.Context) (bool, error) {
	if p.kind == "claude" {
		return p.app.bindings.ClaudeMaintenance.Refresh(ctx)
	}
	return p.app.bindings.CodexUpgrade.RefreshRuntimeAfterMaintenance(ctx)
}

type backendMaintenancePublisher struct {
	app  *App
	kind string
}

func (p backendMaintenancePublisher) Publish(ctx context.Context, progress backendmaintenance.Progress) {
	spec := upgraderender.CodexSpec
	if p.kind == "claude" {
		spec = upgraderender.ClaudeSpec
	}
	var card map[string]any
	if progress.Upgrade != nil {
		card = p.app.bindings.UpgradePresentation.renderUpgradeOperationCard(spec, progress.Operation.SessionKey, *progress.Upgrade)
	}
	if progress.Restart != nil {
		card = p.app.bindings.UpgradePresentation.renderRestartOperationCard(spec, progress.Operation.SessionKey, *progress.Restart)
	}
	patchMaintenanceCard(p.app, progress.Operation.MessageID, card, "backend maintenance progress patch failed")
}

func BackendMaintenancePorts(a *App, kind string) (func() backendmaintenance.Installer, func() string, backendmaintenance.Runtime, backendmaintenance.Publisher) {
	installer := func() backendmaintenance.Installer {
		if kind == "claude" {
			return newClaudeInstallManager(a.cfg.Claude.Command)
		}
		return newCodexInstallManager(a.cfg.Codex.Command)
	}
	busy := func() string {
		if kind == "claude" {
			return a.bindings.Maintenance.ClaudeUpgradeRuntimeBusyReason()
		}
		return a.bindings.Maintenance.CodexUpgradeRuntimeBusyReason()
	}
	return installer, busy, backendMaintenanceRuntime{a, kind}, backendMaintenancePublisher{a, kind}
}

func AsyncExecutor(asyncrunner func(func())) func(func()) { return asyncrunner }

func ClaudeMaintenancePorts(cfg *config.Config, mu *sync.RWMutex, contextFn func() context.Context, owner *frontendruntime.FrontendOwner, frontendConfigIndex int, createCore func(config.ClaudeConfig) ClaudeCore) (func(context.Context) error, func() bool, func() interface{ Close() error }, func()) {
	view := frontendConfigView{cfg: cfg, mu: mu, frontendConfigIndex: frontendConfigIndex}
	snapshot := func() *config.Config {
		if cfg == nil {
			return nil
		}
		if mu != nil {
			mu.RLock()
			defer mu.RUnlock()
		}
		return config.Clone(cfg)
	}
	smoke := func(ctx context.Context) error {
		current := snapshot()
		lifetime := context.Background()
		if contextFn != nil {
			lifetime = contextFn()
		}
		if current == nil {
			return clauderuntime.Smoke(ctx, lifetime, config.ClaudeConfig{}, ".")
		}
		workdir := "."
		for _, ws := range current.Workspaces {
			if cwd := strings.TrimSpace(ws.Cwd); cwd != "" {
				workdir = cwd
				break
			}
		}
		return clauderuntime.Smoke(ctx, lifetime, current.Claude, workdir)
	}
	active := func() bool {
		current := view
		if owner != nil {
			current.backend = owner.Backend()
		}
		return current.configuredBackend() == "claude"
	}
	currentCore := func() interface{ Close() error } {
		if owner == nil {
			return nil
		}
		return owner.ClaudeCore()
	}
	create := func() {
		current := snapshot()
		if current != nil && owner != nil && createCore != nil {
			owner.SetClaudeCore(createCore(current.Claude))
		}
	}
	return smoke, active, currentCore, create
}
