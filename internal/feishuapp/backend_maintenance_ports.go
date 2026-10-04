package feishuapp

import (
	"context"
	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	frontendruntime "feidex/internal/runtime"
	clauderuntime "feidex/internal/runtime/claude"
	codexruntime "feidex/internal/runtime/codex"
	"log/slog"
	"strings"
	"sync"
)

type backendMaintenanceRuntime struct {
	kind              string
	codexUpgrade      codexruntime.UpgradeService
	claudeMaintenance *clauderuntime.Maintenance
}

func (p backendMaintenanceRuntime) Refresh(ctx context.Context) (bool, error) {
	if p.kind == "claude" {
		if p.claudeMaintenance == nil {
			return false, nil
		}
		return p.claudeMaintenance.Refresh(ctx)
	}
	return p.codexUpgrade.RefreshRuntimeAfterMaintenance(ctx)
}

type backendMaintenancePublisher struct {
	frontend      identity.FrontendID
	kind          string
	renderUpgrade func(string, appbackend.BackendUpgradeSnapshot) map[string]any
	renderRestart func(string, appbackend.BackendRestartSnapshot) map[string]any
	patchCard     func(context.Context, string, map[string]any) error
}

func (p backendMaintenancePublisher) Publish(ctx context.Context, progress backendmaintenance.Progress) {
	var card map[string]any
	if progress.Upgrade != nil && p.renderUpgrade != nil {
		card = p.renderUpgrade(progress.Operation.SessionKey, *progress.Upgrade)
	}
	if progress.Restart != nil && p.renderRestart != nil {
		card = p.renderRestart(progress.Operation.SessionKey, *progress.Restart)
	}
	if strings.TrimSpace(progress.Operation.MessageID) == "" || card == nil || p.patchCard == nil {
		return
	}
	if err := p.patchCard(ctx, progress.Operation.MessageID, card); err != nil {
		slog.Warn("backend maintenance progress patch failed",
			"frontend_id", string(p.frontend), "kind", p.kind,
			"message_id", progress.Operation.MessageID, "error", err)
	}
}

type BackendMaintenancePortValues struct {
	Config            *config.Config
	ConfigMu          *sync.RWMutex
	Kind              string
	Frontend          identity.FrontendID
	State             backendmaintenance.MaintenanceStateService
	CodexUpgrade      codexruntime.UpgradeService
	ClaudeMaintenance *clauderuntime.Maintenance
	RenderUpgrade     func(string, appbackend.BackendUpgradeSnapshot) map[string]any
	RenderRestart     func(string, appbackend.BackendRestartSnapshot) map[string]any
	PatchCard         func(context.Context, string, map[string]any) error
}

func BackendMaintenancePorts(values BackendMaintenancePortValues) (func() backendmaintenance.Installer, func() string, backendmaintenance.Runtime, backendmaintenance.Publisher) {
	command := func() string {
		if values.Config == nil {
			return ""
		}
		if values.ConfigMu != nil {
			values.ConfigMu.RLock()
			defer values.ConfigMu.RUnlock()
		}
		if values.Kind == "claude" {
			return values.Config.Claude.Command
		}
		return values.Config.Codex.Command
	}
	installer := func() backendmaintenance.Installer {
		if values.Kind == "claude" {
			return newClaudeInstallManager(command())
		}
		return newCodexInstallManager(command())
	}
	busy := func() string {
		if values.Kind == "claude" {
			return values.State.ClaudeUpgradeRuntimeBusyReason()
		}
		return values.State.CodexUpgradeRuntimeBusyReason()
	}
	runtime := backendMaintenanceRuntime{
		kind: values.Kind, codexUpgrade: values.CodexUpgrade,
		claudeMaintenance: values.ClaudeMaintenance,
	}
	publisher := backendMaintenancePublisher{
		frontend: values.Frontend, kind: values.Kind,
		renderUpgrade: values.RenderUpgrade, renderRestart: values.RenderRestart,
		patchCard: values.PatchCard,
	}
	return installer, busy, runtime, publisher
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
