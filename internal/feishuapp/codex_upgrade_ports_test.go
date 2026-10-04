package feishuapp

import (
	"context"
	"sync"
	"testing"

	"feidex/internal/config"
	domainbackend "feidex/internal/domain/backend"
	frontendruntime "feidex/internal/runtime"
	codexruntime "feidex/internal/runtime/codex"
)

func TestCodexUpgradePortsUseExplicitDynamicFrontendOwners(t *testing.T) {
	cfg := config.Default()
	mu := &sync.RWMutex{}
	owner := frontendruntime.NewFrontendOwner()
	owner.SetBackend(domainbackend.BackendCodex)
	first := &fakeCodexClient{}
	owner.SetCodexClient(first)
	recovery := codexruntime.NewRecoveryService(codexruntime.RecoveryDependencies{
		State: owner.CodexRecovery,
		IsBackendActive: func() bool {
			return owner.Backend() == domainbackend.BackendCodex
		},
	})

	var gotConfig config.CodexConfig
	originalFactory := newCodexClient
	newCodexClient = func(got config.CodexConfig) CodexClient {
		gotConfig = got
		return &fakeCodexClient{}
	}
	defer func() { newCodexClient = originalFactory }()

	startupRecoveryCalls := 0
	deps := CodexUpgradePorts(
		cfg, mu, "frontend-a", -1, owner,
		BackendRuntimeDeps{codexRecovery: recovery}, recovery,
		func() { startupRecoveryCalls++ },
		func(context.Context) error { return nil },
	)

	mu.Lock()
	cfg.Codex.Command = "codex-next"
	cfg.Codex.ExperimentalAPI = true
	mu.Unlock()
	if !deps.ClientExperimentalAPI() {
		t.Fatal("experimental API setting did not follow the current config")
	}
	if deps.CreateClient() == nil || gotConfig.Command != "codex-next" {
		t.Fatalf("created client config = %+v", gotConfig)
	}
	if !deps.IsBackendActive() {
		t.Fatal("Codex backend should be active")
	}
	owner.SetBackend(domainbackend.BackendClaude)
	if deps.IsBackendActive() {
		t.Fatal("backend activity did not follow the frontend runtime owner")
	}
	owner.SetBackend(domainbackend.BackendCodex)

	if got := deps.CurrentClient(); got != first {
		t.Fatalf("current client = %T, want original client", got)
	}
	second := &fakeCodexClient{}
	if got := deps.ReplaceClient(second); got != first {
		t.Fatalf("replaced client = %T, want original client", got)
	}
	if got := recovery.CurrentClient(); got != second {
		t.Fatalf("recovery current client = %T, want replacement", got)
	}
	deps.RecoverFrontendRuntimeState()
	if startupRecoveryCalls != 1 {
		t.Fatalf("startup recovery calls = %d, want 1", startupRecoveryCalls)
	}

	configured := &fakeCodexClient{}
	deps.ConfigureClient(configured)
	configured.mu.Lock()
	hasErrorHandler := configured.onError != nil
	configured.mu.Unlock()
	if !hasErrorHandler {
		t.Fatal("configured client did not receive runtime transport handling")
	}
}
