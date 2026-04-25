package app

import (
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
				Backend:       config.RuntimeBackendCodex,
				AppID:         "cli_codex",
				AppSecret:     "secret-1",
				ReplyInThread: true,
				Quiet:         config.QuietModeProgress,
			},
		},
		{
			ID: "claude-main",
			FeishuConfig: config.FeishuConfig{
				Backend:       config.RuntimeBackendClaude,
				AppID:         "cli_claude",
				AppSecret:     "secret-2",
				ReplyInThread: true,
				Quiet:         config.QuietModeProgress,
			},
		},
	}

	svc, err := NewService(cfg, filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if len(svc.apps) != 2 {
		t.Fatalf("NewService() apps = %d, want 2", len(svc.apps))
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

	codexApp := svc.apps[0]
	claudeApp := svc.apps[1]
	if codexApp.store != claudeApp.store {
		t.Fatal("frontend apps should share one store")
	}
	if codexApp.frontendID != "codex-main" || codexApp.backend != backendCodex || codexApp.codex != codexClients[0] || codexApp.claude != nil {
		t.Fatalf("codex app = %+v", codexApp)
	}
	if claudeApp.frontendID != "claude-main" || claudeApp.backend != backendClaude || claudeApp.codex != nil || claudeApp.claude != claudeClients[0] {
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
			AppID:         "cli_unset",
			AppSecret:     "secret-1",
			ReplyInThread: true,
			Quiet:         config.QuietModeProgress,
		},
	}}

	svc, err := NewService(cfg, filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if len(svc.apps) != 1 {
		t.Fatalf("NewService() apps = %d, want 1", len(svc.apps))
	}
	if codexCalls != 0 || claudeCalls != 0 {
		t.Fatalf("runtime constructors should not run for unset backend, codex=%d claude=%d", codexCalls, claudeCalls)
	}
	if svc.apps[0].backend != "" || svc.apps[0].codex != nil || svc.apps[0].claude != nil {
		t.Fatalf("unset backend app = %+v", svc.apps[0])
	}
}
