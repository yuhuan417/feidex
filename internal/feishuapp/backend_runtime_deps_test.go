package feishuapp

import (
	"sync"
	"testing"

	"feidex/internal/config"
	"feidex/internal/runtime"
)

func TestBackendRuntimeDepsTracksCurrentBackend(t *testing.T) {
	cfg := config.Default()
	cfg.Feishu.Backend = "codex"
	var configMu sync.RWMutex
	owner := runtime.NewFrontendOwner()
	deps := BackendRuntimeDeps{
		view:    frontendConfigView{cfg: cfg, mu: &configMu},
		runtime: runtimeView{owner: owner},
	}

	owner.SetBackend("claude")
	if got := deps.currentBackend().view.configuredBackend(); got != "claude" {
		t.Fatalf("current backend = %q, want runtime override claude", got)
	}

	owner.SetBackend("")
	if got := deps.currentBackend().view.configuredBackend(); got != "codex" {
		t.Fatalf("current backend = %q, want configured fallback codex", got)
	}
}
