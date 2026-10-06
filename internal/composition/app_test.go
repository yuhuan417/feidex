package composition

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"feidex/internal/config"
	"feidex/internal/feishuapp"
	"feidex/internal/runtime"
)

func TestNewServiceBuildsConfiguredBackendRuntimes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		backends []string
		legacy   bool
	}{
		{name: "legacy_claude", backends: []string{"claude"}, legacy: true},
		{name: "claude", backends: []string{"claude"}},
		{name: "codex", backends: []string{"codex"}},
		{name: "mixed_frontends", backends: []string{"codex", "claude"}},
		{name: "claude_frontends", backends: []string{"claude", "claude"}},
		{name: "codex_frontends", backends: []string{"codex", "codex"}},
		{name: "unset", backends: []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.Default()
			cfg.DataDir = filepath.Join(dir, "data")
			cfg.Workspaces[0].Cwd = dir
			// Construction must not start CLI processes or need credentials.
			cfg.Codex.Command = filepath.Join(dir, "unused-codex")
			cfg.Claude.Command = filepath.Join(dir, "unused-claude")
			if tc.legacy {
				cfg.Feishu.Backend = tc.backends[0]
			} else {
				for i, backend := range tc.backends {
					cfg.Frontends = append(cfg.Frontends, config.FrontendConfig{
						ID: fmt.Sprintf("bot-%d", i), FeishuConfig: config.FeishuConfig{Backend: backend},
					})
				}
			}
			var owners []*runtime.FrontendOwner
			service, err := NewService(cfg, filepath.Join(dir, "config.toml"), func(scope FrontendScope) (*feishuapp.Frontend, error) {
				owners = append(owners, scope.RuntimeOwner)
				return NewFrontend(scope)
			})
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := service.Stop(ctx); err != nil {
					t.Errorf("Stop() error = %v", err)
				}
			})
			if len(service.Frontends) != len(tc.backends) {
				t.Fatalf("frontend count = %d, want %d", len(service.Frontends), len(tc.backends))
			}
			for i, backend := range tc.backends {
				owner := owners[i]
				if got := owner.Backend(); got != backend {
					t.Errorf("frontend %d backend = %q, want %q", i, got, backend)
				}
				if got := owner.ClaudeCore() != nil; got != (backend == "claude") {
					t.Errorf("frontend %d has Claude runtime = %v, backend = %q", i, got, backend)
				}
				if got := owner.CodexClient() != nil; got != (backend == "codex") {
					t.Errorf("frontend %d has Codex client = %v, backend = %q", i, got, backend)
				}
				for _, previous := range owners[:i] {
					if owner.ClaudeCore() != nil && owner.ClaudeCore() == previous.ClaudeCore() {
						t.Errorf("frontend %d shares its Claude runtime", i)
					}
					if owner.CodexClient() != nil && owner.CodexClient() == previous.CodexClient() {
						t.Errorf("frontend %d shares its Codex client", i)
					}
				}
			}
		})
	}
}
