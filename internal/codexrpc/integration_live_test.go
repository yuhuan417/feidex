//go:build integration

package codexrpc

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"feidex/internal/config"
)

func liveCodexConfigFromEnv(t *testing.T) (config.CodexConfig, string) {
	t.Helper()

	cfg := config.CodexConfig{
		Command:         firstNonEmptyEnv("FEIDEX_CODEX_COMMAND", "codex"),
		Transport:       strings.TrimSpace(os.Getenv("FEIDEX_CODEX_TRANSPORT")),
		WSURL:           strings.TrimSpace(os.Getenv("FEIDEX_CODEX_WS_URL")),
		WSBearerToken:   strings.TrimSpace(os.Getenv("FEIDEX_CODEX_WS_BEARER_TOKEN")),
		ExperimentalAPI: true,
		ServiceName:     "feidex-integration",
	}
	if strings.TrimSpace(cfg.Transport) == "" {
		if strings.TrimSpace(cfg.WSURL) != "" {
			cfg.Transport = "ws"
		} else {
			cfg.Transport = "stdio"
		}
	}
	cwd := strings.TrimSpace(os.Getenv("FEIDEX_CODEX_CWD"))
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd() error = %v", err)
		}
		cwd = wd
	}
	return cfg, cwd
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return ""
}

func TestLiveCodexInitializeModelListAndThreadRead(t *testing.T) {
	cfg, cwd := liveCodexConfigFromEnv(t)

	client := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := client.Start(ctx, true); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	var models ModelListResult
	if err := client.Call(ctx, "model/list", map[string]any{
		"limit":         20,
		"includeHidden": false,
	}, &models); err != nil {
		t.Fatalf("model/list error = %v", err)
	}
	if len(models.Data) == 0 {
		t.Fatal("model/list returned no visible models")
	}

	var thread ThreadStartResult
	if err := client.Call(ctx, "thread/start", map[string]any{
		"cwd":                    cwd,
		"approvalPolicy":         "on-request",
		"sandbox":                "workspace-write",
		"serviceName":            cfg.ServiceName,
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
	}, &thread); err != nil {
		t.Fatalf("thread/start error = %v", err)
	}
	if strings.TrimSpace(thread.Thread.ID) == "" {
		t.Fatalf("thread/start result = %+v, want non-empty thread id", thread)
	}

	var read ThreadReadResult
	if err := client.Call(ctx, "thread/read", map[string]any{
		"threadId":     thread.Thread.ID,
		"includeTurns": true,
	}, &read); err != nil {
		t.Fatalf("thread/read error = %v", err)
	}
	if read.Thread.ID != thread.Thread.ID {
		t.Fatalf("thread/read id = %q, want %q", read.Thread.ID, thread.Thread.ID)
	}
	if strings.TrimSpace(read.Thread.Cwd) == "" {
		t.Fatalf("thread/read cwd = %q, want non-empty cwd", read.Thread.Cwd)
	}
}
