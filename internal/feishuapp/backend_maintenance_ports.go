package feishuapp

import (
	"context"
	"feidex/internal/adapter/feishu/upgraderender"
	"feidex/internal/application/backendmaintenance"
	clauderuntime "feidex/internal/runtime/claude"
	"strings"
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

func ClaudeMaintenancePorts(a *App) (func(context.Context) error, func() bool, func() interface{ Close() error }, func()) {
	smoke := func(ctx context.Context) error {
		cfg := modelConfigReadCopy(a)
		workdir := "."
		for _, ws := range cfg.Workspaces {
			if cwd := strings.TrimSpace(ws.Cwd); cwd != "" {
				workdir = cwd
				break
			}
		}
		return clauderuntime.Smoke(ctx, a.Context(), cfg.Claude, workdir)
	}
	active := func() bool { return configuredBackend(a) == "claude" }
	current := func() interface{ Close() error } { return currentClaudeCore(a) }
	create := func() { setClaudeCore(a, a.bindings.ClaudeFactory(modelConfigReadCopy(a).Claude)) }
	return smoke, active, current, create
}
