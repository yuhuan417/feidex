package app

import (
	"path/filepath"
	"strings"
	"testing"

	"feidex/internal/app/modelconfig"
	"feidex/internal/claudecli"
	"feidex/internal/codexrpc"
	"feidex/internal/config"
	"feidex/internal/feishu"
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
	model, effort := modelconfig.EffectiveConfiguredModelAndEffort(cfg, result)
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
	if err := newModelConfigService(a).updateGlobalModelConfig(func(c *config.CodexConfig) {
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
	cfg.Feishu.Backend = backendCodex
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
	a := &App{cfg: cfg, backend: backendCodex}
	body := newBackendConfigurationService(a).statusCardBody(sess)
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
	card := newModelConfigService(a).renderModelConfigCard(codexrpc.ModelListResult{
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

func TestRenderClaudeModelConfigCardUsesSelectStaticPickers(t *testing.T) {
	cfg := config.Default()
	cfg.Feishu.Backend = backendClaude
	cfg.Claude.Model = "mimo-v2-pro"
	cfg.Claude.Effort = "high"
	a := &App{cfg: cfg, backend: backendClaude}

	card := newModelConfigService(a).renderClaudeModelConfigCard("sess-1", "menu.model")
	selects := cardSelectStaticForTest(card)
	if len(selects) != 2 {
		t.Fatalf("claude model config selects = %+v, want 2 select_static elements", selects)
	}

	options, _ := selects[0]["options"].([]map[string]any)
	values := map[string]bool{}
	for _, option := range options {
		value, _ := option["value"].(string)
		values[value] = true
	}
	for _, want := range []string{"sonnet", "opus", "haiku", "mimo-v2-pro"} {
		if !values[want] {
			t.Fatalf("claude model picker missing %q: %+v", want, options)
		}
	}

	body := cardMarkdownContent(t, card)
	for _, want := range []string{
		"当前 backend: `claude`",
		"/model set <model-id>",
		"frontend 空闲",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("claude model config body missing %q: %q", want, body)
		}
	}
}

func TestStatusCardBodyUsesClaudeModelAndEffortOnClaudeBackend(t *testing.T) {
	cfg := config.Default()
	cfg.Feishu.Backend = backendClaude
	cfg.Claude.Model = "mimo-v2-pro"
	cfg.Claude.Effort = "max"
	a := &App{cfg: cfg, backend: backendClaude}

	body := newBackendConfigurationService(a).statusCardBody(&state.Session{WorkspaceID: "default"})
	if !strings.Contains(body, "Claude model: `mimo-v2-pro`") {
		t.Fatalf("status body missing Claude model: %q", body)
	}
	if !strings.Contains(body, "Claude effort: `max`") {
		t.Fatalf("status body missing Claude effort: %q", body)
	}
}

func TestRenderModelMenuCardForClaudeOmitsFast(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	a.cfg.Claude.Model = "mimo-v2-pro"
	a.cfg.Claude.Effort = "high"

	card := newBackendConfigurationService(a).renderModelMenuCard("sess-1")
	body := cardMarkdownContent(t, card)
	if !strings.Contains(body, "当前 effort: `high`") {
		t.Fatalf("Claude model menu body = %q", body)
	}

	actions := cardButtonsForTest(card)
	seen := map[string]bool{}
	for _, action := range actions {
		value, _ := action["value"].(map[string]any)
		if len(value) == 0 {
			behaviors, _ := action["behaviors"].([]map[string]any)
			if len(behaviors) > 0 {
				value, _ = behaviors[0]["value"].(map[string]any)
			}
		}
		name, _ := value["action"].(string)
		seen[name] = true
	}
	if !seen["menu.model"] {
		t.Fatalf("Claude model menu actions = %+v, want menu.model", actions)
	}
	if seen["menu.fast"] {
		t.Fatalf("Claude model menu should omit menu.fast: %+v", actions)
	}
}

func TestRenderModelMenuCardWithoutBackendDoesNotFallbackToCodex(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = ""
	a.cfg.Feishu.Backend = ""

	card := newBackendConfigurationService(a).renderModelMenuCard("sess-1")
	body := cardMarkdownContent(t, card)
	if !strings.Contains(body, "还没有设置 backend") {
		t.Fatalf("unset backend model menu body = %q, want backend selection prompt", body)
	}
	if strings.Contains(body, "当前 fast:") || strings.Contains(body, "当前 reasoning:") {
		t.Fatalf("unset backend model menu body = %q, should not render Codex model fields", body)
	}
}

func TestUpdateClaudeModelConfigDoesNotResetIdleRuntimeSession(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a.claude = claude

	sessionKey := "feishu:p2p:chat:user"
	if err := a.store.UpsertSession(&state.Session{
		Key:                     sessionKey,
		WorkspaceID:             a.cfg.Workspaces[0].ID,
		ActiveThreadID:          "claude-thread-1",
		ActiveThreadWorkspaceID: a.cfg.Workspaces[0].ID,
		OwnerUserID:             "user",
		ChatID:                  "chat",
		ChatType:                "p2p",
		Status:                  "idle",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}

	if err := newModelConfigService(a).updateClaudeModelConfig(func(c *config.ClaudeConfig) {
		c.Model = "opus"
		c.Effort = "max"
	}); err != nil {
		t.Fatalf("updateClaudeModelConfig() error = %v", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after config update")
	}
	if sess.ActiveThreadID != "claude-thread-1" || sess.ActiveThreadWorkspaceID != a.cfg.Workspaces[0].ID {
		t.Fatalf("session after Claude config update = %+v, want thread preserved", sess)
	}
	if claude.resetCalls != 0 {
		t.Fatalf("Claude reset calls = %d, want no reset", claude.resetCalls)
	}
}

func TestCompleteClaudeModelSetHotAppliesCurrentSession(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	sessionKey := "feishu:p2p:chat:user"
	claude := &fakeClaudeCore{setModelApplied: true}
	a.claude = claude

	resp, err := newModelConfigService(a).completeClaudeModelSet(&feishu.CardAction{
		ActionValue: map[string]any{
			"session_key": sessionKey,
			"menu_action": "menu.model",
		},
	}, "opus")
	if err != nil {
		t.Fatalf("completeClaudeModelSet() error = %v", err)
	}
	if resp == nil || resp.Toast == nil {
		t.Fatal("completeClaudeModelSet() missing toast")
	}
	if resp.Toast.Type != "success" || !strings.Contains(resp.Toast.Content, "当前会话与后续对话会使用新配置") {
		t.Fatalf("completeClaudeModelSet() toast = %#v", resp.Toast)
	}
	if got := a.cfg.Claude.Model; got != "opus" {
		t.Fatalf("Claude model = %q, want opus", got)
	}
	if len(claude.setModelCalls) != 1 || claude.setModelCalls[0].sessionKey != sessionKey || claude.setModelCalls[0].model != "opus" {
		t.Fatalf("Claude SetModel calls = %+v", claude.setModelCalls)
	}
	if claude.resetCalls != 0 {
		t.Fatalf("Claude reset calls = %d, want none", claude.resetCalls)
	}
}

func TestCompleteClaudeEffortSetHotAppliesCurrentSession(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	sessionKey := "feishu:p2p:chat:user"
	claude := &fakeClaudeCore{setEffortApplied: true}
	a.claude = claude

	resp, err := newModelConfigService(a).completeClaudeEffortSet(&feishu.CardAction{
		ActionValue: map[string]any{
			"session_key": sessionKey,
			"menu_action": "menu.model",
		},
	}, "high")
	if err != nil {
		t.Fatalf("completeClaudeEffortSet() error = %v", err)
	}
	if resp == nil || resp.Toast == nil {
		t.Fatal("completeClaudeEffortSet() missing toast")
	}
	if resp.Toast.Type != "success" || !strings.Contains(resp.Toast.Content, "当前会话与后续对话会使用新配置") {
		t.Fatalf("completeClaudeEffortSet() toast = %#v", resp.Toast)
	}
	if got := a.cfg.Claude.Effort; got != "high" {
		t.Fatalf("Claude effort = %q, want high", got)
	}
	if len(claude.setEffortCalls) != 1 || claude.setEffortCalls[0].sessionKey != sessionKey || claude.setEffortCalls[0].effort != "high" {
		t.Fatalf("Claude SetEffort calls = %+v", claude.setEffortCalls)
	}
	if claude.resetCalls != 0 {
		t.Fatalf("Claude reset calls = %d, want none", claude.resetCalls)
	}
}

func TestCompleteClaudeEffortSetDefaultWarnsWhenLiveSessionCannotClear(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	sessionKey := "feishu:p2p:chat:user"
	claude := &fakeClaudeCore{setEffortErr: claudecli.ErrEffortDefaultHotApplyUnsupported}
	a.claude = claude

	resp, err := newModelConfigService(a).completeClaudeEffortSet(&feishu.CardAction{
		ActionValue: map[string]any{
			"session_key": sessionKey,
			"menu_action": "menu.model",
		},
	}, "")
	if err != nil {
		t.Fatalf("completeClaudeEffortSet(default) error = %v", err)
	}
	if resp == nil || resp.Toast == nil {
		t.Fatal("completeClaudeEffortSet(default) missing toast")
	}
	if resp.Toast.Type != "warning" || !strings.Contains(resp.Toast.Content, "暂不支持热切回默认") {
		t.Fatalf("completeClaudeEffortSet(default) toast = %#v", resp.Toast)
	}
	if got := a.cfg.Claude.Effort; got != "" {
		t.Fatalf("Claude effort = %q, want empty default", got)
	}
	if len(claude.setEffortCalls) != 1 || claude.setEffortCalls[0].sessionKey != sessionKey || claude.setEffortCalls[0].effort != "" {
		t.Fatalf("Claude SetEffort calls = %+v", claude.setEffortCalls)
	}
	if claude.resetCalls != 0 {
		t.Fatalf("Claude reset calls = %d, want none", claude.resetCalls)
	}
}

func TestUpdateClaudeModelConfigRejectsActiveFrontend(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.backend = backendClaude
	a.cfg.Feishu.Backend = backendClaude
	claude := &fakeClaudeCore{}
	a.claude = claude

	sessionKey := "sess-1"
	sub := seedActiveSubmission(t, a, sessionKey, "claude-thread-1", "claude-turn-1")
	newRuntimeStateService(a).bindTurnSubmission("claude-thread-1", "claude-turn-1", sessionKey, sub.ID)

	if _, err := a.store.UpdateSession(sessionKey, func(sess *state.Session) {
		sess.ActiveThreadWorkspaceID = a.cfg.Workspaces[0].ID
	}); err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}

	if err := newModelConfigService(a).updateClaudeModelConfig(func(c *config.ClaudeConfig) {
		c.Model = "haiku"
	}); err == nil || !strings.Contains(err.Error(), "frontend 空闲") {
		t.Fatalf("updateClaudeModelConfig() error = %v, want frontend idle rejection", err)
	}

	sess := a.store.GetSession(sessionKey)
	if sess == nil {
		t.Fatal("session missing after rejected config update")
	}
	if sess.ActiveThreadID != "claude-thread-1" {
		t.Fatalf("session should keep active thread after rejection: %+v", sess)
	}
	if claude.resetCalls != 0 {
		t.Fatalf("Claude reset should not run after rejection, got %d", claude.resetCalls)
	}
	if got := a.cfg.Claude.Model; got == "haiku" {
		t.Fatalf("Claude model should stay unchanged after rejection, got %q", got)
	}
}
