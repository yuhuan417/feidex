package feishuapp

import (
	"context"
	"sync"
	"testing"

	"feidex/internal/config"
	frontendruntime "feidex/internal/runtime"
)

func TestClaudeMaintenancePortsUseCurrentRuntimeAndConfig(t *testing.T) {
	cfg := config.Default()
	mu := &sync.RWMutex{}
	owner := frontendruntime.NewFrontendOwner()
	owner.SetBackend("codex")
	firstCore, replacementCore := &fakeClaudeCore{}, &fakeClaudeCore{}
	owner.SetClaudeCore(firstCore)
	created := []config.ClaudeConfig{}
	smoke, active, current, create := ClaudeMaintenancePorts(cfg, mu, context.Background, owner, -1, func(got config.ClaudeConfig) ClaudeCore {
		created = append(created, got)
		return replacementCore
	})
	if smoke == nil {
		t.Fatal("smoke function was nil")
	}
	if active() {
		t.Fatal("maintenance was active while the runtime selected Codex")
	}
	owner.SetBackend("claude")
	if !active() {
		t.Fatal("maintenance was inactive while the runtime selected Claude")
	}
	if current() != firstCore {
		t.Fatal("current core did not follow the runtime owner")
	}
	owner.SetClaudeCore(replacementCore)
	if current() != replacementCore {
		t.Fatal("current core retained an old runtime")
	}
	mu.Lock()
	cfg.Claude.Model = "updated-model"
	mu.Unlock()
	create()
	if len(created) != 1 || created[0].Model != "updated-model" || owner.ClaudeCore() != replacementCore {
		t.Fatalf("create used config/core = %+v / %T", created, owner.ClaudeCore())
	}
}
