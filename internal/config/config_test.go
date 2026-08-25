package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestFeishuDomainNormalizationAndBaseURL(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		baseURL string
		wantErr bool
	}{
		{input: "", want: FeishuDomainFeishu, baseURL: "https://open.feishu.cn"},
		{input: "feishu", want: FeishuDomainFeishu, baseURL: "https://open.feishu.cn"},
		{input: "  LARK ", want: FeishuDomainLark, baseURL: "https://open.larksuite.com"},
		{input: "Feishu", want: FeishuDomainFeishu, baseURL: "https://open.feishu.cn"},
		{input: "slack", wantErr: true},
	}
	for _, tc := range cases {
		cfg := FeishuConfig{Domain: tc.input}
		err := normalizeFeishuConfig(&cfg)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("normalizeFeishuConfig(domain=%q) expected error", tc.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeFeishuConfig(domain=%q) error = %v", tc.input, err)
		}
		if cfg.Domain != tc.want {
			t.Fatalf("domain %q normalized to %q, want %q", tc.input, cfg.Domain, tc.want)
		}
		if got := cfg.OpenBaseURL(); got != tc.baseURL {
			t.Fatalf("domain %q OpenBaseURL() = %q, want %q", tc.input, got, tc.baseURL)
		}
	}
}

func TestNormalizeLogLevel(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "info"},
		{"debug", "debug"},
		{"INFO", "info"},
		{"warning", "warn"},
		{"warn", "warn"},
		{"error", "error"},
	}
	for _, tc := range cases {
		got, err := NormalizeLogLevel(tc.input)
		if err != nil {
			t.Fatalf("NormalizeLogLevel(%q) returned error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeLogLevel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeLogLevelRejectsUnsupportedValue(t *testing.T) {
	if _, err := NormalizeLogLevel("trace"); err == nil {
		t.Fatal("expected unsupported log level to return error")
	}
}

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
	}
	for _, tc := range cases {
		got, err := ParseLogLevel(tc.input)
		if err != nil {
			t.Fatalf("ParseLogLevel(%q) returned error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("ParseLogLevel(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestDefaultConfigUsesInfoLogLevel(t *testing.T) {
	cfg := Default()
	if cfg.Log.Level != "info" {
		t.Fatalf("default log level = %q, want info", cfg.Log.Level)
	}
	if cfg.Daemon.ServiceName != "feidex" {
		t.Fatalf("default daemon service name = %q, want feidex", cfg.Daemon.ServiceName)
	}
}

func TestSaveUsesPrivatePermissions(t *testing.T) {
	cfg := Default()
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(file) error = %v", err)
	}
	if got := fileInfo.Mode().Perm(); got&0o077 != 0 {
		t.Fatalf("config file perms = %#o, want no group/world access", got)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(dir) error = %v", err)
	}
	if got := dirInfo.Mode().Perm(); got&0o077 != 0 {
		t.Fatalf("config dir perms = %#o, want no group/world access", got)
	}
}
