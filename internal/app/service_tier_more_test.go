package app

import (
	"strings"
	"testing"

	"feidex/internal/config"
	"feidex/internal/feishu"
	"feidex/internal/state"
)

func TestServiceTierHelpersAndMenu(t *testing.T) {
	if got := normalizeServiceTier(" FAST "); got != serviceTierFast {
		t.Fatalf("normalizeServiceTier() = %q, want fast", got)
	}
	if got := normalizeServiceTier("safe"); got != "" {
		t.Fatalf("normalizeServiceTier(unsupported) = %q, want empty", got)
	}
	if got := toggleServiceTier(""); got != serviceTierFast {
		t.Fatalf("toggleServiceTier(empty) = %q, want fast", got)
	}
	if got := toggleServiceTier(serviceTierFast); got != "" {
		t.Fatalf("toggleServiceTier(fast) = %q, want empty", got)
	}
	if got := renderServiceTierValue(""); got != "-" {
		t.Fatalf("renderServiceTierValue(empty) = %q, want -", got)
	}
	if got := renderServiceTierReplyValue(serviceTierFast); got != "`fast`" {
		t.Fatalf("renderServiceTierReplyValue(fast) = %q", got)
	}

	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	cfg := config.Default()
	a := &App{cfg: cfg, store: store, feishu: feishu.New(cfg.Feishu)}

	card := renderServiceTierMenuCard(a,"sess-1")
	if body := cardElementsForTest(card)[0]["content"].(string); !strings.Contains(body, "当前没有活动线程") {
		t.Fatalf("renderServiceTierMenuCard(no thread) = %q", body)
	}

	if err := a.store.UpsertSession(&state.Session{
		Key:                     "sess-1",
		WorkspaceID:             "default",
		ActiveThreadID:          "thread-1",
		ActiveThreadWorkspaceID: "default",
		ActiveThreadServiceTier: serviceTierFast,
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	card = renderServiceTierMenuCard(a,"sess-1")
	rows := cardElementsForTest(card)
	var primaryFound bool
	for _, row := range rows[1:] {
		columns, _ := row["columns"].([]map[string]any)
		for _, column := range columns {
			elements, _ := column["elements"].([]map[string]any)
			for _, elem := range elements {
				if tag, _ := elem["tag"].(string); tag == "button" && elem["type"] == "primary" {
					primaryFound = true
				}
			}
		}
	}
	if !primaryFound {
		t.Fatalf("service tier rows = %#v, want selected primary button", rows)
	}
}

func TestSetThreadServiceTierAndCommandFastValidation(t *testing.T) {
	store, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("Open(store) error = %v", err)
	}
	a := &App{store: store, feishu: &fakeFeishuClient{}, cfg: config.Default()}

	if _, err := setThreadServiceTier(a,"sess-1", "thread-1", serviceTierFast); err == nil || !strings.Contains(err.Error(), "没有活动线程") {
		t.Fatalf("setThreadServiceTier(no thread) error = %v", err)
	}
	if err := a.store.UpsertSession(&state.Session{
		Key:            "sess-1",
		WorkspaceID:    "default",
		ActiveThreadID: "thread-1",
	}); err != nil {
		t.Fatalf("UpsertSession() error = %v", err)
	}
	if _, err := setThreadServiceTier(a,"sess-1", "thread-x", serviceTierFast); err == nil || !strings.Contains(err.Error(), "已失效") {
		t.Fatalf("setThreadServiceTier(stale) error = %v", err)
	}
	sess, err := setThreadServiceTier(a,"sess-1", "thread-1", serviceTierFast)
	if err != nil || sess.ActiveThreadServiceTier != serviceTierFast {
		t.Fatalf("setThreadServiceTier() = %+v, %v", sess, err)
	}

	if err := commandFast(a,nil, []string{"extra"}); err == nil {
		t.Fatal("expected commandFast(args) to fail")
	}
	if err := commandFast(a,nil, nil); err != nil {
		t.Fatalf("commandFast(nil msg) error = %v", err)
	}
}
