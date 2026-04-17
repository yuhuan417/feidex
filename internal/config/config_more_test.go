package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeFillsDefaultsAndResolvesPaths(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "workspace")
	cfg := &Config{
		DataDir: "data",
		Log: LogConfig{
			Level: " warning ",
		},
		Codex: CodexConfig{
			WSURL:           "wss://example.test/ws",
			Model:           " gpt-5 ",
			ReasoningEffort: " high ",
		},
		Workspaces: []Workspace{
			{
				ID:  " default ",
				Cwd: "./repo",
			},
		},
	}

	if err := cfg.Normalize(baseDir); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	if cfg.Codex.Command != "codex" {
		t.Fatalf("Codex.Command = %q, want codex", cfg.Codex.Command)
	}
	if cfg.Codex.Transport != "ws" {
		t.Fatalf("Codex.Transport = %q, want ws", cfg.Codex.Transport)
	}
	if cfg.Codex.Model != "gpt-5" || cfg.Codex.ReasoningEffort != "high" {
		t.Fatalf("unexpected codex trimming result: %+v", cfg.Codex)
	}
	if cfg.Log.Level != "warn" {
		t.Fatalf("Log.Level = %q, want warn", cfg.Log.Level)
	}
	if cfg.Feishu.Quiet != QuietModeVerbose {
		t.Fatalf("Feishu.Quiet = %q, want verbose", cfg.Feishu.Quiet)
	}
	if cfg.Workspaces[0].ID != "default" {
		t.Fatalf("Workspace.ID = %q, want default", cfg.Workspaces[0].ID)
	}
	if cfg.Workspaces[0].Name != "default" {
		t.Fatalf("Workspace.Name = %q, want default", cfg.Workspaces[0].Name)
	}
	if cfg.Workspaces[0].ApprovalPolicy != "on-request" || cfg.Workspaces[0].SandboxMode != "workspace-write" {
		t.Fatalf("unexpected workspace defaults: %+v", cfg.Workspaces[0])
	}
	if !filepath.IsAbs(cfg.Workspaces[0].Cwd) || !strings.HasSuffix(cfg.Workspaces[0].Cwd, filepath.Join("workspace", "repo")) {
		t.Fatalf("Workspace.Cwd = %q, want absolute workspace path", cfg.Workspaces[0].Cwd)
	}
	if !filepath.IsAbs(cfg.DataDir) || !strings.HasSuffix(cfg.DataDir, filepath.Join("workspace", "data")) {
		t.Fatalf("DataDir = %q, want absolute data path", cfg.DataDir)
	}
}

func TestNormalizeRejectsInvalidWorkspaceConfigurations(t *testing.T) {
	cases := []struct {
		name string
		cfg  *Config
	}{
		{
			name: "missing workspace",
			cfg:  &Config{},
		},
		{
			name: "missing id",
			cfg: &Config{
				Workspaces: []Workspace{{Cwd: "."}},
			},
		},
		{
			name: "missing cwd",
			cfg: &Config{
				Workspaces: []Workspace{{ID: "default"}},
			},
		},
		{
			name: "duplicate ids",
			cfg: &Config{
				Workspaces: []Workspace{
					{ID: "dup", Cwd: "."},
					{ID: "dup", Cwd: "./other"},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Normalize(t.TempDir()); err == nil {
				t.Fatal("expected Normalize() to return an error")
			}
		})
	}
}

func TestSaveLoadAndFindWorkspaceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := &Config{
		DataDir: ".feidex-data",
		Log: LogConfig{
			Level: "info",
		},
		Feishu: FeishuConfig{
			Quiet: QuietModeNormal,
		},
		Codex: CodexConfig{
			Command:   "codex",
			Transport: "stdio",
		},
		Workspaces: []Workspace{
			{ID: "default", Name: "Default", Cwd: ".", ApprovalPolicy: "on-request", SandboxMode: "workspace-write"},
			{ID: "repo", Name: "Repo", Cwd: "./repo", ApprovalPolicy: "never", SandboxMode: "danger-full-access"},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.DataDir == "" || !filepath.IsAbs(loaded.DataDir) {
		t.Fatalf("loaded.DataDir = %q, want absolute path", loaded.DataDir)
	}
	if loaded.Feishu.Quiet != QuietModeNormal {
		t.Fatalf("loaded.Feishu.Quiet = %q, want normal", loaded.Feishu.Quiet)
	}
	if ws := FindWorkspace(loaded, "repo"); ws == nil || ws.Name != "Repo" {
		t.Fatalf("FindWorkspace(repo) = %+v, want Repo", ws)
	}
	if FindWorkspace(loaded, "missing") != nil {
		t.Fatal("FindWorkspace(missing) should return nil")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(content), "[[workspace]]") {
		t.Fatalf("saved config missing workspace sections:\n%s", string(content))
	}
}

func TestLoadMissingFileAndSaveNilConfig(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.toml")); err == nil {
		t.Fatal("expected Load() on missing file to fail")
	}
	if err := Save(filepath.Join(t.TempDir(), "config.toml"), nil); err == nil {
		t.Fatal("expected Save(nil) to fail")
	}
}

func TestLoadLegacyConfigWithoutDaemonSectionKeepsDefaultDaemonServiceName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := strings.Join([]string{
		`data_dir = ".feidex-data"`,
		``,
		`[log]`,
		`level = "info"`,
		``,
		`[feishu]`,
		`app_id = "app-id"`,
		`app_secret = "app-secret"`,
		``,
		`[codex]`,
		`command = "codex"`,
		`transport = "stdio"`,
		``,
		`[[workspace]]`,
		`id = "default"`,
		`name = "Default"`,
		`cwd = "."`,
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(legacy config without daemon section) error = %v", err)
	}
	if cfg.Daemon.ServiceName != "feidex" {
		t.Fatalf("Daemon.ServiceName = %q, want default feidex", cfg.Daemon.ServiceName)
	}
	if cfg.Codex.ServiceName != "feidex" {
		t.Fatalf("Codex.ServiceName = %q, want default feidex", cfg.Codex.ServiceName)
	}
	if len(cfg.Workspaces) != 1 || cfg.Workspaces[0].ID != "default" {
		t.Fatalf("Workspaces = %+v, want preserved legacy workspace", cfg.Workspaces)
	}
}

func TestQuietModeValidationAndConfigFallback(t *testing.T) {
	if got, err := NormalizeQuietMode(""); err != nil || got != QuietModeVerbose {
		t.Fatalf("NormalizeQuietMode(empty) = %q, %v", got, err)
	}
	if got, err := NormalizeQuietMode("progress"); err != nil || got != QuietModeProgress {
		t.Fatalf("NormalizeQuietMode(progress) = %q, %v", got, err)
	}
	if got, err := NormalizeQuietMode("normal"); err != nil || got != QuietModeNormal {
		t.Fatalf("NormalizeQuietMode(normal) = %q, %v", got, err)
	}
	if got, err := ParseQuietMode("final"); err != nil || got != QuietModeFinal {
		t.Fatalf("ParseQuietMode(final) = %q, %v", got, err)
	}
	if _, err := ParseQuietMode("folded"); err == nil {
		t.Fatal("ParseQuietMode(folded) should fail")
	}

	for _, tc := range []struct {
		name      string
		quietLine string
	}{
		{name: "legacy_string", quietLine: `quiet = "folded"`},
		{name: "legacy_bool", quietLine: `quiet = true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			content := strings.Join([]string{
				`data_dir = ".feidex-data"`,
				``,
				`[log]`,
				`level = "info"`,
				``,
				`[feishu]`,
				tc.quietLine,
				``,
				`[codex]`,
				`command = "codex"`,
				`transport = "stdio"`,
				``,
				`[[workspace]]`,
				`id = "default"`,
				`name = "Default"`,
				`cwd = "."`,
			}, "\n")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("WriteFile(config) error = %v", err)
			}
			loaded, err := Load(path)
			if err != nil {
				t.Fatalf("Load(invalid quiet value) error = %v", err)
			}
			if loaded.Feishu.Quiet != QuietModeNormal {
				t.Fatalf("invalid quiet value loaded as %q, want normal fallback", loaded.Feishu.Quiet)
			}
		})
	}
}
