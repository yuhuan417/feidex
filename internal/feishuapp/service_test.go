package feishuapp

import (
	domainbackend "feidex/internal/domain/backend"
	"path/filepath"
	"testing"

	"feidex/internal/config"
)

func TestNewServiceBuildsFrontendScopedApps(t *testing.T) {
	origCodex := newCodexClient
	origFeishu := newFeishuClient
	origClaude := newClaudeCore
	defer func() {
		newCodexClient = origCodex
		newFeishuClient = origFeishu
		newClaudeCore = origClaude
	}()

	codexClients := []*fakeCodexClient{}
	newCodexClient = func(config.CodexConfig) CodexClient {
		client := &fakeCodexClient{}
		codexClients = append(codexClients, client)
		return client
	}
	feishuAppIDs := []string{}
	newFeishuClient = func(cfg config.FeishuConfig) FeishuClient {
		feishuAppIDs = append(feishuAppIDs, cfg.AppID)
		return &fakeFeishuClient{}
	}
	claudeClients := []*fakeClaudeCore{}
	newClaudeCore = func(_ *App, _ config.ClaudeConfig) ClaudeCore {
		client := &fakeClaudeCore{}
		claudeClients = append(claudeClients, client)
		return client
	}

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfg.Frontends = []config.FrontendConfig{
		{
			ID: "codex-main",
			FeishuConfig: config.FeishuConfig{
				Backend:   config.RuntimeBackendCodex,
				AppID:     "cli_codex",
				AppSecret: "secret-1",
				Quiet:     config.QuietModeProgress,
			},
		},
		{
			ID: "claude-main",
			FeishuConfig: config.FeishuConfig{
				Backend:   config.RuntimeBackendClaude,
				AppID:     "cli_claude",
				AppSecret: "secret-2",
				Quiet:     config.QuietModeProgress,
			},
		},
	}

	apps, err := newTestService(cfg, filepath.Join(t.TempDir(), "config.toml"))
	svc := struct{ Frontends []*App }{Frontends: apps}
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if len(svc.Frontends) != 2 {
		t.Fatalf("NewService() apps = %d, want 2", len(svc.Frontends))
	}
	if len(codexClients) != 1 {
		t.Fatalf("newCodexClient calls = %d, want 1", len(codexClients))
	}
	if len(claudeClients) != 1 {
		t.Fatalf("newClaudeCore calls = %d, want 1", len(claudeClients))
	}
	if got := feishuAppIDs; len(got) != 2 || got[0] != "cli_codex" || got[1] != "cli_claude" {
		t.Fatalf("newFeishuClient app_ids = %+v", got)
	}

	codexApp := svc.Frontends[0]
	claudeApp := svc.Frontends[1]
	if codexApp.store != claudeApp.store {
		t.Fatal("frontend apps should share one store")
	}
	if codexApp.ConfigMu() != claudeApp.ConfigMu() {
		t.Fatal("frontend apps should share one config mutex")
	}
	codexClient := codexApp.runtimeOwner.CodexClient()
	claudeOnCodex := codexApp.runtimeOwner.ClaudeCore()
	if codexApp.frontendID != "codex-main" || codexApp.Backend() != domainbackend.BackendCodex || codexClient != codexClients[0] || claudeOnCodex != nil {
		t.Fatalf("codex app = %+v", codexApp)
	}
	codexOnClaude := claudeApp.runtimeOwner.CodexClient()
	claudeClient := claudeApp.runtimeOwner.ClaudeCore()
	if claudeApp.frontendID != "claude-main" || claudeApp.Backend() != domainbackend.BackendClaude || codexOnClaude != nil || claudeClient != claudeClients[0] {
		t.Fatalf("claude app = %+v", claudeApp)
	}
}

func TestNewServiceAllowsUnsetFrontendBackend(t *testing.T) {
	origCodex := newCodexClient
	origFeishu := newFeishuClient
	origClaude := newClaudeCore
	defer func() {
		newCodexClient = origCodex
		newFeishuClient = origFeishu
		newClaudeCore = origClaude
	}()

	codexCalls := 0
	newCodexClient = func(config.CodexConfig) CodexClient {
		codexCalls++
		return &fakeCodexClient{}
	}
	claudeCalls := 0
	newClaudeCore = func(_ *App, _ config.ClaudeConfig) ClaudeCore {
		claudeCalls++
		return &fakeClaudeCore{}
	}
	newFeishuClient = func(cfg config.FeishuConfig) FeishuClient {
		return &fakeFeishuClient{}
	}

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Workspaces[0].Cwd = t.TempDir()
	cfg.Frontends = []config.FrontendConfig{{
		ID: "unset-main",
		FeishuConfig: config.FeishuConfig{
			AppID:     "cli_unset",
			AppSecret: "secret-1",
			Quiet:     config.QuietModeProgress,
		},
	}}

	apps, err := newTestService(cfg, filepath.Join(t.TempDir(), "config.toml"))
	svc := struct{ Frontends []*App }{Frontends: apps}
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if len(svc.Frontends) != 1 {
		t.Fatalf("NewService() apps = %d, want 1", len(svc.Frontends))
	}
	if codexCalls != 0 || claudeCalls != 0 {
		t.Fatalf("runtime constructors should not run for unset backend, codex=%d claude=%d", codexCalls, claudeCalls)
	}
	codexClient := svc.Frontends[0].runtimeOwner.CodexClient()
	claudeClient := svc.Frontends[0].runtimeOwner.ClaudeCore()
	if svc.Frontends[0].Backend() != "" || codexClient != nil || claudeClient != nil {
		t.Fatalf("unset backend app = %+v", svc.Frontends[0])
	}
}
