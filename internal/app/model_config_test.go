package app

import (
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/state"
)

func TestEffectiveConfiguredModelAndEffortUsesCatalogDefaultsWhenUnset(t *testing.T) {
	cfg := config.Default()
	result := codexrpc.ModelListResult{
		Data: []codexrpc.ModelListEntry{
			{
				ID:                     "gpt-5.3-codex",
				IsDefault:              true,
				DefaultReasoningEffort: "medium",
				SupportedReasoningEfforts: []codexrpc.ModelReasoningEffortEntry{
					{ReasoningEffort: "low"},
					{ReasoningEffort: "medium"},
				},
			},
		},
	}
	model, effort := effectiveConfiguredModelAndEffort(cfg, result)
	if model == nil || model.ID != "gpt-5.3-codex" {
		t.Fatalf("unexpected default model: %#v", model)
	}
	if effort != "medium" {
		t.Fatalf("unexpected default effort: %q", effort)
	}
}

func TestUpdateGlobalModelConfigClearsUnsupportedEffort(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	a := &App{cfg: cfg, cfgPath: cfgPath}
	result := codexrpc.ModelListResult{
		Data: []codexrpc.ModelListEntry{
			{
				ID:                     "model-a",
				IsDefault:              true,
				DefaultReasoningEffort: "medium",
				SupportedReasoningEfforts: []codexrpc.ModelReasoningEffortEntry{
					{ReasoningEffort: "medium"},
				},
			},
			{
				ID:                     "model-b",
				DefaultReasoningEffort: "low",
				SupportedReasoningEfforts: []codexrpc.ModelReasoningEffortEntry{
					{ReasoningEffort: "low"},
				},
			},
		},
	}
	a.cfg.Codex.ReasoningEffort = "medium"
	if err := a.updateGlobalModelConfig(func(c *config.CodexConfig) {
		c.Model = "model-b"
	}, result); err != nil {
		t.Fatalf("updateGlobalModelConfig: %v", err)
	}
	if a.cfg.Codex.Model != "model-b" {
		t.Fatalf("unexpected configured model: %q", a.cfg.Codex.Model)
	}
	if a.cfg.Codex.ReasoningEffort != "" {
		t.Fatalf("expected unsupported effort to be cleared, got %q", a.cfg.Codex.ReasoningEffort)
	}
}

func TestStatusCardBodyShowsWorkspaceThreadAndEffectiveSettings(t *testing.T) {
	cfg := config.Default()
	cfg.Codex.Model = "gpt-5.4"
	cfg.Codex.ReasoningEffort = "high"
	sess := &state.Session{
		WorkspaceID:                "default",
		ActiveThreadID:             "thread-1",
		ActiveThreadSandboxMode:    "read-only",
		ActiveThreadApprovalPolicy: "untrusted",
		Status:                     "turn_in_progress",
		Queue:                      []string{"sub-1"},
	}
	a := &App{cfg: cfg}
	body := a.statusCardBody(sess)
	for _, want := range []string{
		"版本: `0.1.0`",
		"log level: `info`",
		"workspace sandbox: `workspace-write`",
		"workspace policy: `on-request`",
		"thread sandbox: `read-only`",
		"thread policy: `untrusted`",
		"thread service tier: -",
		"生效 sandbox: `read-only`",
		"生效 policy: `untrusted`",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in status body, got %q", want, body)
		}
	}
}

func TestRenderModelConfigCardUsesSelectStaticPickers(t *testing.T) {
	cfg := config.Default()
	a := &App{cfg: cfg}
	card := a.renderModelConfigCard(codexrpc.ModelListResult{
		Data: []codexrpc.ModelListEntry{
			{
				ID:                     "gpt-5",
				DisplayName:            "GPT-5",
				DefaultReasoningEffort: "medium",
				SupportedReasoningEfforts: []codexrpc.ModelReasoningEffortEntry{
					{ReasoningEffort: "low"},
					{ReasoningEffort: "medium"},
				},
				IsDefault: true,
			},
		},
	}, "sess-1", "menu.model")
	if got := cardSelectStaticForTest(card); len(got) != 2 {
		t.Fatalf("model config selects = %+v, want 2 select_static elements", got)
	}
}
