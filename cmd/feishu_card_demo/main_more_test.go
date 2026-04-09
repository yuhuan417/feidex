package main

import (
	"strings"
	"testing"
	"time"

	"feidex/internal/feishu"
)

func TestParseOptionsAppliesDefaultsAndRejectsInvalidValues(t *testing.T) {
	origNow := timeNow
	defer func() { timeNow = origNow }()
	timeNow = func() time.Time { return time.Unix(1700000100, 0) }

	opts, err := parseOptions([]string{"--chat-id", "oc_1", "--kind", "permissions", "--color", "", "--reason", ""})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if opts.Title != "权限请求" {
		t.Fatalf("Title = %q, want 权限请求", opts.Title)
	}
	if opts.Color != "orange" || opts.RequestID == "" || opts.Timeout != 15*time.Second {
		t.Fatalf("parseOptions() defaults = %+v, want color/request id/timeout", opts)
	}
	if !strings.HasPrefix(opts.RequestID, "demo-permissions-") {
		t.Fatalf("RequestID = %q, want generated demo prefix", opts.RequestID)
	}

	if _, err := parseOptions([]string{"--chat-id", "oc_1", "--timeout", "0"}); err == nil || !strings.Contains(err.Error(), "greater than 0") {
		t.Fatalf("parseOptions(timeout) error = %v, want invalid timeout", err)
	}
	if _, err := parseOptions([]string{"--chat-id", "oc_1", "--kind", "bad"}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("parseOptions(kind) error = %v, want unsupported kind", err)
	}
}

func TestLegacyCardHelpers(t *testing.T) {
	if got := defaultCardDemoTitle("command"); got != "等待审批" {
		t.Fatalf("defaultCardDemoTitle(command) = %q", got)
	}
	if got := defaultCardDemoTitle("file"); got != "文件审批" {
		t.Fatalf("defaultCardDemoTitle(file) = %q", got)
	}
	if got := defaultCardDemoTitle("other"); got != "卡片 Demo" {
		t.Fatalf("defaultCardDemoTitle(default) = %q", got)
	}

	if !usesLegacyCardDemo("plain") || usesLegacyCardDemo("agent") {
		t.Fatal("usesLegacyCardDemo() returned unexpected result")
	}

	if got := defaultCardDemoBody("file", "", ""); !strings.Contains(got, "文件变更审批") {
		t.Fatalf("defaultCardDemoBody(file) = %q", got)
	}
	if got := defaultCardDemoBody("command", "", ""); !strings.Contains(got, "`pwd`") {
		t.Fatalf("defaultCardDemoBody(command) = %q", got)
	}

	if got := cardDemoButtons("plain", "req-1"); len(got) != 2 {
		t.Fatalf("cardDemoButtons(plain) count = %d, want 2", len(got))
	}
	if got := cardDemoButtons("permissions", "req-1"); len(got) != 2 {
		t.Fatalf("cardDemoButtons(permissions) count = %d, want 2", len(got))
	}
	if got := cardDemoButtons("command", "req-1"); len(got) != 3 {
		t.Fatalf("cardDemoButtons(command) count = %d, want 3", len(got))
	}

	card := newLegacyMarkdownCard(" Title ", "", " body ")
	header := card["header"].(map[string]any)
	if got := header["template"].(string); got != "blue" {
		t.Fatalf("header template = %q, want blue", got)
	}
	appendLegacyCardElement(card, buildLegacyCardActionElement([]feishu.Button{{Text: "Open", Type: "primary", Name: "open", Value: map[string]any{"id": "1"}}}))
	elements := card["elements"].([]map[string]any)
	if len(elements) != 2 || elements[1]["tag"] != "action" {
		t.Fatalf("legacy card elements = %#v, want markdown + action", elements)
	}
	actions := elements[1]["actions"].([]map[string]any)
	if actions[0]["name"] != "open" {
		t.Fatalf("action name = %#v, want open", actions[0]["name"])
	}
}

func TestMaskAppID(t *testing.T) {
	if got := maskAppID("abc"); got != "abc" {
		t.Fatalf("maskAppID(short) = %q, want unchanged", got)
	}
	if got := maskAppID("cli_a1b2c3"); got != "cli...2c3" {
		t.Fatalf("maskAppID(long) = %q, want masked", got)
	}
}
