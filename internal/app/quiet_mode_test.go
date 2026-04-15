package app

import (
	"context"
	"path/filepath"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestShouldDeliverTurnKindInQuiet(t *testing.T) {
	allowed := []string{"final_message", "turn_output", "turn_plan", "turn_queued", "turn_terminal"}
	for _, kind := range allowed {
		if !shouldDeliverTurnKindInQuiet(kind) {
			t.Fatalf("expected kind %q to be allowed in quiet mode", kind)
		}
	}
	blocked := []string{"turn_reasoning", "turn_command_execution", "turn_file_change", "turn_item"}
	for _, kind := range blocked {
		if shouldDeliverTurnKindInQuiet(kind) {
			t.Fatalf("expected kind %q to be blocked in quiet mode", kind)
		}
	}
}

func TestShouldDeliverTurnItemInQuiet(t *testing.T) {
	if !shouldDeliverTurnItemInQuiet("agent_message") {
		t.Fatal("expected non-final agent_message item to be allowed")
	}
	if !shouldDeliverTurnItemInQuiet("agent_message") {
		t.Fatal("expected final agent_message item to be allowed")
	}
	if !shouldDeliverTurnItemInQuiet("plan") {
		t.Fatal("expected plan item to be allowed")
	}
	if shouldDeliverTurnItemInQuiet("reasoning") {
		t.Fatal("expected reasoning item to be blocked")
	}
	if shouldDeliverTurnItemInQuiet("command_execution") {
		t.Fatal("expected command_execution item to be blocked")
	}
}

func TestUpdateQuietModePersistsConfig(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	a := &App{cfg: cfg, cfgPath: cfgPath}
	if err := a.updateQuietMode(true); err != nil {
		t.Fatalf("updateQuietMode: %v", err)
	}
	if !a.cfg.Feishu.Quiet {
		t.Fatal("expected in-memory quiet mode enabled")
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !loaded.Feishu.Quiet {
		t.Fatal("expected persisted quiet mode enabled")
	}
}

func TestCommandQuietTogglesAndSupportsConfigCard(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	ff := &fakeFeishuClient{}
	a := &App{cfg: cfg, cfgPath: cfgPath, feishu: ff}
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat", ChatType: "p2p"}
	if err := a.commandQuiet(msg, nil); err != nil {
		t.Fatalf("commandQuiet() error = %v", err)
	}
	if !a.quietModeEnabled() {
		t.Fatal("expected /quiet to toggle quiet mode")
	}
	if len(ff.replyTexts) != 1 {
		t.Fatalf("reply text count = %d, want 1", len(ff.replyTexts))
	}
	if err := a.commandQuiet(msg, []string{"config"}); err != nil {
		t.Fatalf("commandQuiet(config) error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
}

func TestSendTurnItemCardAllowsAgentMessageInQuietMode(t *testing.T) {
	a, ff, _ := newTestApp(t)
	a.cfg.Feishu.Quiet = true
	sub := &state.Submission{
		ID:               "sub-1",
		SessionKey:       "sess-1",
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         "thread-1",
		TurnID:           "turn-1",
		TriggerMessageID: "msg-1",
	}
	payload := turnItemCardPayload{
		ItemID:      "item-1",
		ItemType:    "agent_message",
		Title:       "回复",
		Color:       "green",
		SummaryText: "intermediate reply",
	}

	if got := a.sendTurnItemCard(context.Background(), sub, payload); got != "reply-card-id" {
		t.Fatalf("sendTurnItemCard() = %q, want reply-card-id", got)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count = %d, want 1", len(ff.replyCards))
	}
}
