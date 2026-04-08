package app

import (
	"path/filepath"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
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

func TestShouldDeliverTurnSnapshotInQuiet(t *testing.T) {
	if shouldDeliverTurnSnapshotInQuiet(turnItemSnapshot{ItemType: "agent_message"}) {
		t.Fatal("expected non-final agent_message snapshot to be blocked")
	}
	if !shouldDeliverTurnSnapshotInQuiet(turnItemSnapshot{ItemType: "agent_message", IsFinalAnswer: true}) {
		t.Fatal("expected final agent_message snapshot to be allowed")
	}
	if !shouldDeliverTurnSnapshotInQuiet(turnItemSnapshot{ItemType: "plan"}) {
		t.Fatal("expected plan snapshot to be allowed")
	}
	if shouldDeliverTurnSnapshotInQuiet(turnItemSnapshot{ItemType: "reasoning"}) {
		t.Fatal("expected reasoning snapshot to be blocked")
	}
	if shouldDeliverTurnSnapshotInQuiet(turnItemSnapshot{ItemType: "command_execution"}) {
		t.Fatal("expected command_execution snapshot to be blocked")
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

func TestCommandQuietTogglesWithoutCard(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, feishu: &fakeFeishuClient{}}
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat", ChatType: "p2p"}
	if err := a.commandQuiet(msg, nil); err != nil {
		t.Fatalf("commandQuiet() error = %v", err)
	}
	if !a.quietModeEnabled() {
		t.Fatal("expected quiet mode to be enabled after toggle")
	}
}
