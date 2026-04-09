package app

import (
	"strings"
	"testing"

	"feidex/internal/config"
)

func TestBuildOutboundCardDemoValidatesAndBuildsKinds(t *testing.T) {
	cfg := config.Default()
	cfg.Workspaces[0].Cwd = t.TempDir()

	if _, _, err := BuildOutboundCardDemo(nil, OutboundCardDemoOptions{Kind: "agent"}); err == nil {
		t.Fatal("expected nil config to fail")
	}
	if _, _, err := BuildOutboundCardDemo(cfg, OutboundCardDemoOptions{Kind: "plain"}); err == nil {
		t.Fatal("expected unsupported kind to fail")
	}

	card, kind, err := BuildOutboundCardDemo(cfg, OutboundCardDemoOptions{Kind: "agent"})
	if err != nil {
		t.Fatalf("BuildOutboundCardDemo(agent) error = %v", err)
	}
	if kind != "turn_output" {
		t.Fatalf("resolved kind = %q, want turn_output", kind)
	}
	if _, ok := card["header"]; ok {
		t.Fatalf("turn_output card should omit header: %#v", card["header"])
	}
	body := card["body"].(map[string]any)["elements"].([]map[string]any)[0]["content"].(string)
	if !strings.Contains(body, "普通 agent message") {
		t.Fatalf("turn_output body = %q, want default body", body)
	}

	card, kind, err = BuildOutboundCardDemo(cfg, OutboundCardDemoOptions{Kind: "final", Body: "done"})
	if err != nil {
		t.Fatalf("BuildOutboundCardDemo(final) error = %v", err)
	}
	if kind != "final_message" {
		t.Fatalf("resolved kind = %q, want final_message", kind)
	}
	header := card["header"].(map[string]any)
	if header["template"] != "green" {
		t.Fatalf("final card header = %#v, want green", header)
	}

	card, kind, err = BuildOutboundCardDemo(cfg, OutboundCardDemoOptions{Kind: "turn_plan"})
	if err != nil {
		t.Fatalf("BuildOutboundCardDemo(turn_plan) error = %v", err)
	}
	if kind != "turn_plan" {
		t.Fatalf("resolved kind = %q, want turn_plan", kind)
	}
	body = card["body"].(map[string]any)["elements"].([]map[string]any)[0]["content"].(string)
	if !strings.Contains(body, "[pending]") {
		t.Fatalf("turn_plan body = %q, want default plan text", body)
	}
}

func TestOutboundCardDemoHelpers(t *testing.T) {
	cases := map[string]string{
		"agent":            "turn_output",
		"agent_message":    "turn_output",
		"final":            "final_message",
		"final_agent":      "final_message",
		"turn_terminal":    "turn_terminal",
		"turn_file_change": "turn_file_change",
	}
	for input, want := range cases {
		if got := normalizeOutboundCardDemoKind(input); got != want {
			t.Fatalf("normalizeOutboundCardDemoKind(%q) = %q, want %q", input, got, want)
		}
	}
	if got := normalizeOutboundCardDemoKind("plain"); got != "" {
		t.Fatalf("normalizeOutboundCardDemoKind(plain) = %q, want empty", got)
	}

	if got := defaultOutboundCardDemoBody("turn_command_execution"); !strings.Contains(got, "pwd") {
		t.Fatalf("defaultOutboundCardDemoBody(command) = %q", got)
	}
	if got := defaultOutboundCardDemoBody("turn_file_change"); !strings.Contains(got, "main.go") {
		t.Fatalf("defaultOutboundCardDemoBody(file_change) = %q", got)
	}
	if got := defaultOutboundCardDemoBody("unknown"); !strings.Contains(got, "卡片 demo") {
		t.Fatalf("defaultOutboundCardDemoBody(default) = %q", got)
	}
}
