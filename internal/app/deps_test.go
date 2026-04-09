package app

import (
	"testing"

	"feidex/internal/config"
)

func TestDependencyFactoriesReturnValues(t *testing.T) {
	cfg := config.Default()
	if newCodexClient(cfg.Codex) == nil {
		t.Fatal("newCodexClient() returned nil")
	}
	if newFeishuClient(cfg.Feishu) == nil {
		t.Fatal("newFeishuClient() returned nil")
	}
	if newReleaseClient() == nil {
		t.Fatal("newReleaseClient() returned nil")
	}
	if currentVersion() == "" || currentGOARCH() == "" {
		t.Fatalf("currentVersion/currentGOARCH returned empty values")
	}
}
