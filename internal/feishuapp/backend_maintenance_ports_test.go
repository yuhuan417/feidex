package feishuapp

import (
	"context"
	"sync"
	"testing"

	appbackend "feidex/internal/adapter/feishu/backend"
	"feidex/internal/application/backendmaintenance"
	"feidex/internal/config"
	"feidex/internal/domain/identity"
	codexruntime "feidex/internal/runtime/codex"
)

func TestBackendMaintenancePortsUseExplicitDynamicDependencies(t *testing.T) {
	cfg := config.Default()
	cfg.Codex.Command = "codex-old"
	mu := &sync.RWMutex{}
	manager := &fakeCodexInstallManager{}
	var gotCommand string
	originalFactory := newCodexInstallManager
	newCodexInstallManager = func(command string) codexInstallManager {
		gotCommand = command
		return manager
	}
	defer func() { newCodexInstallManager = originalFactory }()

	refreshCalls := 0
	var patchedMessage string
	var patchedCard map[string]any
	installer, busy, runtime, publisher := BackendMaintenancePorts(BackendMaintenancePortValues{
		Config: cfg, ConfigMu: mu, Kind: "codex", Frontend: identity.FrontendID("frontend-a"),
		State: backendmaintenance.MaintenanceStateService{},
		CodexUpgrade: codexruntime.UpgradeService{
			IsBackendActive: func() bool { return false },
			SmokeTest: func(context.Context) error {
				refreshCalls++
				return nil
			},
		},
		RenderUpgrade: func(sessionKey string, snapshot appbackend.BackendUpgradeSnapshot) map[string]any {
			return map[string]any{"session": sessionKey, "phase": snapshot.Phase}
		},
		PatchCard: func(_ context.Context, messageID string, card map[string]any) error {
			patchedMessage, patchedCard = messageID, card
			return nil
		},
	})

	if got := busy(); got != "" {
		t.Fatalf("busy reason = %q, want empty", got)
	}
	mu.Lock()
	cfg.Codex.Command = "codex-current"
	mu.Unlock()
	if got := installer(); got != manager || gotCommand != "codex-current" {
		t.Fatalf("installer = %T, command = %q", got, gotCommand)
	}
	if changed, err := runtime.Refresh(context.Background()); err != nil || changed || refreshCalls != 1 {
		t.Fatalf("runtime refresh = (%v, %v), calls = %d", changed, err, refreshCalls)
	}
	publisher.Publish(context.Background(), backendmaintenance.Progress{
		Operation: backendmaintenance.Operation{MessageID: "message-1", SessionKey: "session-1"},
		Upgrade:   &appbackend.BackendUpgradeSnapshot{Phase: "installing"},
	})
	if patchedMessage != "message-1" || patchedCard["session"] != "session-1" || patchedCard["phase"] != "installing" {
		t.Fatalf("published card = message %q, card %#v", patchedMessage, patchedCard)
	}
}
