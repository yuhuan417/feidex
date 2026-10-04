package feishuapp

import (
	"sync"
	"testing"

	"feidex/internal/config"
	"feidex/internal/runtime"
)

func TestConversationPortsReadsCurrentBackend(t *testing.T) {
	cfg := config.Default()
	cfg.Feishu.Backend = "codex"
	var configMu sync.RWMutex
	owner := runtime.NewFrontendOwner()
	owner.EffectRunner = &runtime.EffectRunner{}
	deps := ConversationPorts(ConversationPortInputs{
		Config: cfg, ConfigMu: &configMu, RuntimeOwner: owner,
	})

	owner.SetBackend("claude")
	if got := deps.Backend(); got != "claude" {
		t.Fatalf("backend = %q, want runtime override claude", got)
	}

	owner.SetBackend("")
	if got := deps.Backend(); got != "codex" {
		t.Fatalf("backend = %q, want configured fallback codex", got)
	}
}
