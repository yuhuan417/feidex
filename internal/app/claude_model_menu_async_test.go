package app

import (
	"errors"
	"strings"
	"testing"

	"feidex/internal/claudecli"
	"feidex/internal/feishu"
)

// The Claude model menu must answer the card callback without waiting for the
// CLI round-trip, and still hot-apply afterwards.
func TestClaudeModelMenuAppliesRuntimeAsynchronously(t *testing.T) {
	claude := &fakeClaudeCore{}
	a, ff, sessionKey := newClaudePermissionMenuApp(t, claude)

	resp, err := newCardActionService(a).dispatch(&feishu.CardAction{
		UserID:    "user-1",
		ChatID:    "chat-1",
		MessageID: "msg-model-1",
		Option:    "claude-fable-5",
		ActionValue: map[string]any{
			"action":      "model.config.select_model",
			"session_key": sessionKey,
			"menu_action": "menu.model",
		},
	})
	if err != nil {
		t.Fatalf("dispatch(model.config.select_model) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || !strings.Contains(resp.Toast.Content, "后台切换") {
		t.Fatalf("response = %#v, want an immediate toast mentioning the background switch", resp)
	}
	if resp.Card == nil {
		t.Fatal("response should render the model menu immediately")
	}

	a.waitAsync()

	if len(claude.setModelCalls) != 1 {
		t.Fatalf("hot-apply calls = %#v, want one async apply", claude.setModelCalls)
	}
	if got := claude.setModelCalls[0].model; got != "claude-fable-5" {
		t.Fatalf("applied model = %q, want claude-fable-5", got)
	}
	if len(ff.patchedCards) != 0 {
		t.Fatal("no failure patch expected on the happy path")
	}
}

// A hot-apply that fails after the ack must patch the menu card with a warning.
func TestClaudeModelMenuRuntimeFailurePatchesCard(t *testing.T) {
	claude := &fakeClaudeCore{setModelErr: errors.New("control request failed: session gone")}
	a, ff, sessionKey := newClaudePermissionMenuApp(t, claude)

	if _, err := newCardActionService(a).dispatch(&feishu.CardAction{
		UserID:    "user-1",
		ChatID:    "chat-1",
		MessageID: "msg-model-2",
		Option:    "claude-fable-5",
		ActionValue: map[string]any{
			"action":      "model.config.select_model",
			"session_key": sessionKey,
			"menu_action": "menu.model",
		},
	}); err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}

	a.waitAsync()

	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched cards = %d, want the model card patched with a warning", len(ff.patchedCards))
	}
	body := cardMarkdownContent(t, ff.patchedCards[0])
	if !strings.Contains(body, "当前会话热更新失败") || !strings.Contains(body, "配置已保存") {
		t.Fatalf("patched card body = %q, want the runtime failure warning", body)
	}
}

// The slash command path has no ack deadline and keeps applying synchronously.
func TestClaudeModelCommandAppliesRuntimeSynchronously(t *testing.T) {
	claude := &fakeClaudeCore{}
	a, _, _ := newClaudePermissionMenuApp(t, claude)

	if err := newModelConfigService(a).commandClaudeModel(&feishu.InboundMessage{
		MessageID: "msg-model-command",
		ChatID:    "chat-1",
		ChatType:  "p2p",
		UserID:    "user-1",
	}, []string{"set", "claude-fable-5"}); err != nil {
		t.Fatalf("commandClaudeModel(set) error = %v", err)
	}
	if len(claude.setModelCalls) != 1 {
		t.Fatalf("hot-apply calls = %#v, want one synchronous apply", claude.setModelCalls)
	}
}

// Effort changes follow the same rule, including the "cannot hot-apply back to
// default" case that used to be a toast.
func TestClaudeEffortMenuAppliesRuntimeAsynchronously(t *testing.T) {
	claude := &fakeClaudeCore{setEffortErr: claudecli.ErrEffortDefaultHotApplyUnsupported}
	a, ff, sessionKey := newClaudePermissionMenuApp(t, claude)

	resp, err := newCardActionService(a).dispatch(&feishu.CardAction{
		UserID:    "user-1",
		ChatID:    "chat-1",
		MessageID: "msg-effort-1",
		Option:    "default",
		ActionValue: map[string]any{
			"action":      "model.config.select_effort",
			"session_key": sessionKey,
			"menu_action": "menu.model",
		},
	})
	if err != nil {
		t.Fatalf("dispatch(model.config.select_effort) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || !strings.Contains(resp.Toast.Content, "后台切换") {
		t.Fatalf("response = %#v, want an immediate toast mentioning the background switch", resp)
	}

	a.waitAsync()

	if len(claude.setEffortCalls) != 1 {
		t.Fatalf("effort apply calls = %#v, want one async apply", claude.setEffortCalls)
	}
	if len(ff.patchedCards) != 1 {
		t.Fatalf("patched cards = %d, want the unsupported-default case patched", len(ff.patchedCards))
	}
	body := cardMarkdownContent(t, ff.patchedCards[0])
	if !strings.Contains(body, "暂不支持热切回默认") {
		t.Fatalf("patched card body = %q, want the dedicated default-effort warning", body)
	}
}

// Auxiliary model updates restart the session (stopping the CLI process), so
// they must not run on the ack path either.
func TestClaudeAuxiliaryModelMenuRestartsSessionAsynchronously(t *testing.T) {
	claude := &fakeClaudeCore{}
	a, _, sessionKey := newClaudePermissionMenuApp(t, claude)

	resp, err := newCardActionService(a).dispatch(&feishu.CardAction{
		UserID:    "user-1",
		ChatID:    "chat-1",
		MessageID: "msg-aux-1",
		Option:    "deepseek-flash",
		ActionValue: map[string]any{
			"action":      "model.aux_config.select_small_model",
			"session_key": sessionKey,
			"menu_action": "menu.model_auxiliary",
		},
	})
	if err != nil {
		t.Fatalf("dispatch(model.aux_config.select_small_model) error = %v", err)
	}
	if resp == nil || resp.Toast == nil || !strings.Contains(resp.Toast.Content, "后台重启") {
		t.Fatalf("response = %#v, want an immediate toast mentioning the background restart", resp)
	}
	a.waitAsync()

	if claude.resetCalls != 1 {
		t.Fatalf("session resets = %d, want the restart to happen in the background", claude.resetCalls)
	}
}
