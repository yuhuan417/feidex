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
	tests := []struct {
		mode    config.QuietMode
		allowed []string
		blocked []string
	}{
		{
			mode:    config.QuietModeProgress,
			allowed: []string{"final_message", "turn_output", "turn_plan", "turn_queued", "turn_started", "turn_terminal"},
			blocked: []string{"turn_reasoning", "turn_command_execution", "turn_file_change", "turn_item"},
		},
		{
			mode:    config.QuietModeNormal,
			allowed: []string{"final_message", "turn_output", "turn_plan", "turn_started"},
			blocked: []string{"turn_reasoning", "turn_command_execution", "turn_file_change", "turn_item", "turn_queued", "turn_terminal"},
		},
		{
			mode:    config.QuietModeFinal,
			allowed: []string{"final_message"},
			blocked: []string{"turn_output", "turn_plan", "turn_reasoning", "turn_started", "turn_terminal"},
		},
	}

	for _, tc := range tests {
		for _, kind := range tc.allowed {
			if !shouldDeliverTurnKindInQuiet(tc.mode, kind) {
				t.Fatalf("expected kind %q to be allowed in quiet mode %q", kind, tc.mode)
			}
		}
		for _, kind := range tc.blocked {
			if shouldDeliverTurnKindInQuiet(tc.mode, kind) {
				t.Fatalf("expected kind %q to be blocked in quiet mode %q", kind, tc.mode)
			}
		}
	}
}

func TestShouldDeliverTurnItemInQuiet(t *testing.T) {
	if !shouldDeliverTurnItemInQuiet(config.QuietModeProgress, "agent_message", false) {
		t.Fatal("expected non-final agent_message item to be allowed in progress")
	}
	if !shouldDeliverTurnItemInQuiet(config.QuietModeNormal, "plan", false) {
		t.Fatal("expected plan item to be allowed in normal")
	}
	if shouldDeliverTurnItemInQuiet(config.QuietModeNormal, "reasoning", false) {
		t.Fatal("expected reasoning item to be blocked in normal")
	}
	if shouldDeliverTurnItemInQuiet(config.QuietModeFinal, "agent_message", false) {
		t.Fatal("expected non-final agent message to be blocked in final mode")
	}
	if !shouldDeliverTurnItemInQuiet(config.QuietModeFinal, "agent_message", true) {
		t.Fatal("expected final agent message to be allowed in final mode")
	}
}

func TestShouldDeliverTurnItemPayloadInQuietSupportsClaudeTodoWrite(t *testing.T) {
	payload := turnItemCardPayload{
		ItemType:         "dynamic_tool_call",
		ProtocolItemType: "dynamic_tool_call",
		ToolName:         "TodoWrite",
	}
	if !shouldDeliverTurnItemPayloadInQuiet(config.QuietModeProgress, payload) {
		t.Fatal("expected TodoWrite payload to be allowed in progress mode")
	}
	if !shouldDeliverTurnItemPayloadInQuiet(config.QuietModeNormal, payload) {
		t.Fatal("expected TodoWrite payload to be allowed in normal mode")
	}
	if shouldDeliverTurnItemPayloadInQuiet(config.QuietModeFinal, payload) {
		t.Fatal("expected TodoWrite payload to be blocked in final mode")
	}
}

func TestUpdateQuietModePersistsConfig(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	a := &App{cfg: cfg, cfgPath: cfgPath}
	if err := updateQuietMode(a, config.QuietModeNormal); err != nil {
		t.Fatalf("updateQuietMode: %v", err)
	}
	if a.cfg.Feishu.Quiet != config.QuietModeNormal {
		t.Fatalf("expected in-memory quiet mode normal, got %q", a.cfg.Feishu.Quiet)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Feishu.Quiet != config.QuietModeNormal {
		t.Fatalf("expected persisted quiet mode normal, got %q", loaded.Feishu.Quiet)
	}
}

func TestCommandQuietSupportsConfigCardAndExplicitModes(t *testing.T) {
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	ff := &fakeFeishuClient{}
	a := &App{cfg: cfg, cfgPath: cfgPath, feishu: ff}
	msg := &feishu.InboundMessage{MessageID: "m-1", ChatID: "chat", ChatType: "p2p"}
	if err := commandQuiet(a, msg, nil); err != nil {
		t.Fatalf("commandQuiet() error = %v", err)
	}
	if len(ff.replyCards) != 1 {
		t.Fatalf("reply card count after /quiet = %d, want 1", len(ff.replyCards))
	}
	if err := commandQuiet(a, msg, []string{"normal"}); err != nil {
		t.Fatalf("commandQuiet(normal) error = %v", err)
	}
	if quietMode(feishuConfig(a)) != config.QuietModeNormal {
		t.Fatalf("expected /quiet normal to set normal mode, got %q", quietMode(feishuConfig(a)))
	}
	if len(ff.replyTexts) != 1 {
		t.Fatalf("reply text count after /quiet normal = %d, want 1", len(ff.replyTexts))
	}
	if err := commandQuiet(a, msg, []string{"config"}); err != nil {
		t.Fatalf("commandQuiet(config) error = %v", err)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count after /quiet config = %d, want 2", len(ff.replyCards))
	}
}

func TestSendTurnItemCardQuietModes(t *testing.T) {
	a, ff, _ := newTestApp(t)
	sub := &state.Submission{
		ID:               "sub-1",
		SessionKey:       "sess-1",
		WorkspaceID:      a.cfg.Workspaces[0].ID,
		ThreadID:         "thread-1",
		TurnID:           "turn-1",
		TriggerMessageID: "msg-1",
	}

	a.cfg.Feishu.Quiet = config.QuietModeNormal
	agentPayload := turnItemCardPayload{
		ItemID:      "item-1",
		ItemType:    "agent_message",
		Title:       "回复",
		Color:       "green",
		SummaryText: "intermediate reply",
	}
	if got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, agentPayload, ""); got != "reply-card-id" {
		t.Fatalf("sendTurnItemCard(normal agent) = %q, want reply-card-id", got)
	}

	a.cfg.Feishu.Quiet = config.QuietModeFinal
	if got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, agentPayload, ""); got != "" {
		t.Fatalf("sendTurnItemCard(final non-final agent) = %q, want empty", got)
	}

	finalPayload := agentPayload
	finalPayload.IsFinalAnswer = true
	if got := newOutboundCardService(a).sendTurnItemCardWithReuse(context.Background(), sub, finalPayload, ""); got != "reply-card-id" {
		t.Fatalf("sendTurnItemCard(final final-answer) = %q, want reply-card-id", got)
	}
	if len(ff.replyCards) != 2 {
		t.Fatalf("reply card count = %d, want 2", len(ff.replyCards))
	}
}
